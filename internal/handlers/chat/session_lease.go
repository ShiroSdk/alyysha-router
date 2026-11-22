// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync/atomic"
	"time"

	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/log"
	"alyysha/proxy/internal/models"
)

// Cross-provider session affinity (B1 remainder): native client session ids
// (Claude Code, Codex) bind to the connection that served them, so the
// vendor's prompt cache keeps hitting across turns. The binding is a lease —
// persistent, expiring on the vendor window — not the in-memory combo sticky
// state, which only lives per process.
//
// routing.session_affinity = off (default) | combo | global:
//
//	off     — no lease reads or writes; byte-identical to the old path.
//	combo   — affinity applies only to requests walking a combo chain.
//	global  — affinity applies to every provider dispatch.
//
// Affinity only REORDERS candidates inside the already-eligible set of one
// provider: it never promotes a connection from another combo or provider.
const (
	sessionLeaseScope     = "session"
	sessionAffinityOff    = "off"
	sessionAffinityCombo  = "combo"
	sessionAffinityGlobal = "global"
)

var sessionAffinityMode atomic.Value // string

func init() {
	sessionAffinityMode.Store(sessionAffinityOff)
}

// SetSessionAffinityMode applies the stored setting; unknown values read as off.
func SetSessionAffinityMode(mode string) {
	switch mode {
	case sessionAffinityCombo, sessionAffinityGlobal:
		sessionAffinityMode.Store(mode)
	default:
		sessionAffinityMode.Store(sessionAffinityOff)
	}
}

func currentSessionAffinityMode() string {
	if v, ok := sessionAffinityMode.Load().(string); ok {
		return v
	}
	return sessionAffinityOff
}

// sessionLeaseKey hashes the raw session id: the id can carry account
// identifiers and the lease key column is shared across scopes.
func sessionLeaseKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return "session:" + hex.EncodeToString(sum[:])[:16]
}

// vendorAffinityTTL mirrors the vendor prompt-cache windows the sticky TTL
// comment documents (Claude ~5m, Gemini ~60m): the binding should expire
// exactly when the vendor cache it protects would have gone cold.
func vendorAffinityTTL(provider string) time.Duration {
	p := strings.ToLower(provider)
	switch {
	case strings.Contains(p, "claude"), strings.Contains(p, "anthropic"):
		return 5 * time.Minute
	case strings.Contains(p, "gemini"), strings.Contains(p, "antigravity"):
		return 60 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// comboDispatchCtxKey marks requests walking a combo chain (the "combo"
// affinity mode narrows to these).
type comboDispatchCtxKey struct{}

// WithComboDispatch marks the request as walking a combo chain.
func WithComboDispatch(ctx context.Context, comboName string) context.Context {
	return context.WithValue(ctx, comboDispatchCtxKey{}, comboName)
}

// comboDispatchFrom returns the combo name when the request walks one.
func comboDispatchFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	name, _ := ctx.Value(comboDispatchCtxKey{}).(string)
	return name
}

// sessionAffinityApplies gates the layer by mode: global always (when a
// session exists); combo only for requests walking a combo chain.
func sessionAffinityApplies(ctx context.Context) bool {
	switch currentSessionAffinityMode() {
	case sessionAffinityGlobal:
		return true
	case sessionAffinityCombo:
		return comboDispatchFrom(ctx) != ""
	default:
		return false
	}
}

// reorderSessionLease moves the leased connection to the front of the
// candidate list (stable: the rest keeps rotation order). Missing lease or a
// lease naming a connection outside the candidates is a no-op.
func reorderSessionLease(conns []*models.ProviderConnection, leasedConnID string) []*models.ProviderConnection {
	if leasedConnID == "" || len(conns) < 2 {
		return conns
	}
	for i, c := range conns {
		if c != nil && c.ID == leasedConnID {
			if i == 0 {
				return conns
			}
			reordered := make([]*models.ProviderConnection, 0, len(conns))
			reordered = append(reordered, c)
			reordered = append(reordered, conns[:i]...)
			reordered = append(reordered, conns[i+1:]...)
			return reordered
		}
	}
	return conns
}

// applySessionLeaseAffinity reorders the candidates by the session lease when
// the mode applies. Returns the possibly-reordered list.
func (h *ChatHandler) applySessionLeaseAffinity(ctx context.Context, conns []*models.ProviderConnection, provider string) []*models.ProviderConnection {
	if !sessionAffinityApplies(ctx) {
		return conns
	}
	sessionID := handlerutil.GetSessionID(ctx)
	if sessionID == "" || h.Repo == nil {
		return conns
	}
	lease, err := h.Repo.ReadLease(sessionLeaseScope, sessionLeaseKey(sessionID))
	if err != nil || lease == nil || lease.Value == "" {
		return conns
	}
	// A lease naming a connection outside this provider's candidates is
	// stale; the rotation order below applies untouched.
	return reorderSessionLease(conns, lease.Value)
}

// bindSessionLease records which connection served this session, on the
// vendor window TTL. Best-effort: a lease write failure must not fail a turn
// that already succeeded.
func (h *ChatHandler) bindSessionLease(ctx context.Context, provider, sessionID, connID string) {
	if h.Repo == nil || sessionID == "" || connID == "" {
		return
	}
	if err := h.Repo.UpsertLease(sessionLeaseScope, sessionLeaseKey(sessionID), connID, vendorAffinityTTL(provider)); err != nil {
		log.Warn("session affinity", "lease write failed", "provider", provider, "error", err)
	}
}
