// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package freeresilience

import (
	"testing"
	"time"
)

func radarTestTracker(t *testing.T) *Tracker {
	t.Helper()
	tr := NewTracker(FallbackConfig{}, nil, nil)
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		RecordRadarOutcomeForTest(tr, "a", base.Add(time.Duration(i)*time.Minute), true, int64(100+i))
	}
	for i := 0; i < 100; i++ {
		ok := i < 40
		RecordRadarOutcomeForTest(tr, "b", base.Add(time.Duration(i)*time.Minute), ok, int64(5000+i))
	}
	tr.now = func() time.Time { return base.Add(2 * time.Hour) }
	return tr
}

func RecordRadarOutcomeForTest(tr *Tracker, provider string, at time.Time, success bool, latencyMs int64) {
	tr.mu.Lock()
	tr.health[provider] = append(tr.health[provider], Outcome{At: at, Success: success, LatencyMs: latencyMs})
	tr.mu.Unlock()
}

// TestRadarSnapshotAggregation pins the radar math: success rate, p50, p95
// and uptime per provider.
func TestRadarSnapshotAggregation(t *testing.T) {
	tr := radarTestTracker(t)
	rows := tr.RadarSnapshot()
	byName := map[string]RadarRow{}
	for _, r := range rows {
		byName[r.Provider] = r
	}
	a, okA := byName["a"]
	b, okB := byName["b"]
	if !okA || !okB {
		t.Fatalf("radar rows = %+v, want a and b", rows)
	}
	if a.SuccessRate < 0.98 || a.UptimePct < 98 {
		t.Errorf("a = %+v, want ~99%% success/uptime", a)
	}
	if a.P50Ms < 100 || a.P50Ms > 200 || a.P95Ms == 0 {
		t.Errorf("a latency = p50 %v p95 %v, want p50 ~150 (nearest-rank) and a real p95", a.P50Ms, a.P95Ms)
	}
	if b.SuccessRate < 0.39 || b.SuccessRate > 0.41 {
		t.Errorf("b success rate = %v, want ~0.40", b.SuccessRate)
	}
	if b.P50Ms < 5000 {
		t.Errorf("b p50 = %v, want the slow latency", b.P50Ms)
	}
}

// TestCombinedHealthScoreRanking pins the tie-breaker: both providers proven,
// the faster healthier A scores above B; config order still wins upstream.
func TestCombinedHealthScoreRanking(t *testing.T) {
	tr := radarTestTracker(t)
	a := tr.CombinedHealthScore("a")
	b := tr.CombinedHealthScore("b")
	if a <= b {
		t.Errorf("combined score a=%v b=%v, want a > b (healthier + faster)", a, b)
	}
	ordered := OrderCandidates([]string{"b", "a"}, []string{"a", "b"}, tr.CombinedHealthScore)
	if len(ordered) != 2 || ordered[0] != "b" {
		t.Errorf("config order must win: ordered = %v", ordered)
	}
	ordered2 := OrderCandidates(nil, []string{"b", "a"}, tr.CombinedHealthScore)
	if len(ordered2) != 2 || ordered2[0] != "a" {
		t.Errorf("no config order: combined score must rank A first, got %v", ordered2)
	}
}
