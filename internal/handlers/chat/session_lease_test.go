// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"testing"
	"time"

	"9routerx/proxy/internal/db"
	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/models"
)

func leaseTestRepo(t *testing.T) *db.Repo {
	t.Helper()
	database, cleanup := setupChatTestDB(t)
	t.Cleanup(cleanup)
	if err := db.EnsureUpstreamLeases(database); err != nil {
		t.Fatalf("ensure upstream_leases: %v", err)
	}
	return db.NewRepo(database)
}

func leaseConn(id string) *models.ProviderConnection {
	return &models.ProviderConnection{ID: id}
}

// TestSessionLeaseReorder pins the candidate reorder: the leased connection
// moves to the front, the rest keeps rotation order, absent = no-op.
func TestSessionLeaseReorder(t *testing.T) {
	a := leaseConn("a")
	b := leaseConn("b")
	c := leaseConn("c")
	conns := []*models.ProviderConnection{a, b, c}
	reordered := reorderSessionLease(conns, "b")
	if reordered[0] != b || reordered[1] != a || reordered[2] != c {
		t.Errorf("reorder = %s,%s,%s; want b,a,c", reordered[0].ID, reordered[1].ID, reordered[2].ID)
	}
	if same := reorderSessionLease(conns, "a"); same[0] != a {
		t.Error("leased front must stay front")
	}
	if same := reorderSessionLease(conns, "absent"); len(same) != 3 || same[0] != a {
		t.Error("absent lease must be a no-op")
	}
	if same := reorderSessionLease(conns, ""); len(same) != 3 {
		t.Error("empty lease must be a no-op")
	}
}

// TestSessionAffinityModeGate pins (c): off = never applies; combo = only
// combo-walking requests; global = always; unknown = off.
func TestSessionAffinityModeGate(t *testing.T) {
	t.Cleanup(func() { SetSessionAffinityMode(sessionAffinityOff) })
	SetSessionAffinityMode(sessionAffinityOff)
	ctx := WithComboDispatch(context.Background(), "combo-1")
	if sessionAffinityApplies(ctx) {
		t.Error("off mode must not apply")
	}

	SetSessionAffinityMode(sessionAffinityCombo)
	if sessionAffinityApplies(context.Background()) {
		t.Error("combo mode must not apply to non-combo requests")
	}
	if !sessionAffinityApplies(ctx) {
		t.Error("combo mode must apply to combo requests")
	}

	SetSessionAffinityMode(sessionAffinityGlobal)
	if !sessionAffinityApplies(context.Background()) {
		t.Error("global mode must apply everywhere")
	}

	SetSessionAffinityMode("garbage")
	if sessionAffinityApplies(ctx) {
		t.Error("unknown mode falls back to off")
	}
}

// TestVendorAffinityTTL pins the vendor window mapping (b).
func TestVendorAffinityTTL(t *testing.T) {
	if got := vendorAffinityTTL("claude"); got != 5*time.Minute {
		t.Errorf("claude TTL = %v, want 5m", got)
	}
	if got := vendorAffinityTTL("gemini"); got != 60*time.Minute {
		t.Errorf("gemini TTL = %v, want 60m", got)
	}
	if got := vendorAffinityTTL("deepseek"); got != 30*time.Minute {
		t.Errorf("default TTL = %v, want 30m", got)
	}
}

// TestSessionLeaseRoundTrip pins the persistent binding end to end: upsert →
// read → reorder finds the bound connection; expired lease frees the session.
func TestSessionLeaseRoundTrip(t *testing.T) {
	repo := leaseTestRepo(t)
	h := &ChatHandler{Repo: repo}
	SetSessionAffinityMode(sessionAffinityGlobal)
	t.Cleanup(func() { SetSessionAffinityMode(sessionAffinityOff) })
	sessionID := "sess-abc-123"
	h.bindSessionLease(context.Background(), "deepseek", sessionID, "conn-9")

	lease, err := repo.ReadLease(sessionLeaseScope, sessionLeaseKey(sessionID))
	if err != nil || lease == nil {
		t.Fatalf("lease read = %v, %v; want a row", lease, err)
	}
	if lease.Value != "conn-9" {
		t.Errorf("lease value = %q, want conn-9", lease.Value)
	}

	a := leaseConn("conn-1")
	nine := leaseConn("conn-9")
	got := h.applySessionLeaseAffinity(
		handlerutil.WithSessionID(context.Background(), sessionID),
		[]*models.ProviderConnection{a, nine}, "deepseek")
	if got[0] != nine {
		t.Errorf("affinity reorder first = %s, want conn-9", got[0].ID)
	}

	// Force the lease past its window: the binding frees, rotation applies.
	if _, err := repo.RawDB().Exec(
		`UPDATE upstream_leases SET expires_at = ? WHERE scope = ? AND key = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		sessionLeaseScope, sessionLeaseKey(sessionID)); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	got = h.applySessionLeaseAffinity(
		handlerutil.WithSessionID(context.Background(), sessionID),
		[]*models.ProviderConnection{a, nine}, "deepseek")
	if got[0] != a {
		t.Errorf("expired lease reorder first = %s, want original order", got[0].ID)
	}
}
