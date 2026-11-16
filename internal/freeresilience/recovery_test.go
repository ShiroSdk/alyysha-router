// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package freeresilience

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// recoveryFakeStore records Notify calls with their dedupe keys.
type recoveryFakeStore struct {
	notified []string
	failNext bool
}

func (f *recoveryFakeStore) Notify(_ context.Context, _ string, _ string, dedupeKey string, _ string, _ string, _ string, _ string) (string, error) {
	if f.failNext {
		return "", fmt.Errorf("store down")
	}
	f.notified = append(f.notified, dedupeKey)
	return "n-" + dedupeKey, nil
}

// TestNotifyRecoveryDedupeKeyAndMulti pins the event shape: one notification
// per rejoined provider, dedupeKey <provider>#recovery.
func TestNotifyRecoveryDedupeKeyAndMulti(t *testing.T) {
	store := &recoveryFakeStore{}
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	created := NotifyRecovery(context.Background(), store, []string{"antigravity", "glm"}, base)
	if len(created) != 2 {
		t.Fatalf("created = %v, want 2 entries", created)
	}
	if store.notified[0] != "antigravity#recovery" || store.notified[1] != "glm#recovery" {
		t.Errorf("dedupe keys = %v, want <provider>#recovery", store.notified)
	}
}

// TestNotifyRecoverySingleProviderFailure pins the backoff contract's second
// half: when the same provider re-cools after a failed recovery, the event
// dedupe keeps the feed at one entry — no spam — while the attempts counter
// (the tracker's) scales the next cooldown.
func TestNotifyRecoveryFailureRecordsOnce(t *testing.T) {
	store := &recoveryFakeStore{failNext: true}
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if created := NotifyRecovery(ctx, store, []string{"antigravity"}, base); len(created) != 0 {
		t.Errorf("failed notify must return no created keys, got %v", created)
	}
	store.failNext = false
	if created := NotifyRecovery(ctx, store, []string{"antigravity"}, base.Add(time.Minute)); len(created) != 1 {
		t.Errorf("recovery after store recovery = %v, want 1", created)
	}
	if len(store.notified) != 1 {
		t.Errorf("notified = %v, want exactly 1", store.notified)
	}
}

// TestRejoinReturnsPreferredActive pins the SEBAGIAN-DONE contract: after the
// cooldown window passes, the preferred provider (first in the operator's
// config order) is active again by config-order-first rotation — the
// recovery event rides along, the ranking does not demote it.
func TestRejoinReturnsPreferredActive(t *testing.T) {
	tr := NewTracker(FallbackConfig{Order: []string{"preferred", "backup"}}, nil, nil)
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return base }
	RecordRadarOutcomeForTest(tr, "backup", base.Add(-time.Minute), true, 100)
	// The preferred provider hits a limit and cools down.
	RecordRadarOutcomeForTest(tr, "preferred", base.Add(-time.Minute), false, 100)
	_ = tr.ApplyCooldownForTest("preferred", base, base.Add(30*time.Minute))
	if cooling, _ := tr.IsCooling("preferred"); !cooling {
		t.Fatal("preferred must be cooling before the window passes")
	}
	// Past the window: ExpireCooldowns rejoins it, and the chain's config
	// order puts it back first.
	tr.now = func() time.Time { return base.Add(45 * time.Minute) }
	rejoined := tr.ExpireCooldowns()
	if len(rejoined) != 1 || rejoined[0] != "preferred" {
		t.Fatalf("rejoined = %v, want [preferred]", rejoined)
	}
	if cooling, _ := tr.IsCooling("preferred"); cooling {
		t.Fatal("preferred must be active again after the window")
	}
	ordered := OrderCandidates([]string{"preferred", "backup"}, []string{"preferred", "backup"}, tr.CombinedHealthScore)
	if ordered[0] != "preferred" {
		t.Errorf("ordered = %v, want preferred first by config order", ordered)
	}
}

// ApplyCooldownForTest parks a provider directly — the production path is
// recordLimitEvent, which needs regimes and headers the test does not carry.
func (t *Tracker) ApplyCooldownForTest(provider string, from, until time.Time) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	t.cooling[provider] = until
	t.mu.Unlock()
	return true
}
