// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"strings"
	"testing"
)

// TestRoutingExplainRingTwoFallbacks pins (G1.4): a request whose chain took
// two fallbacks lands three entries (attempt, attempt, success) with a reason
// each, and the ring never grows past its bound.
func TestRoutingExplainRingTwoFallbacks(t *testing.T) {
	rec := NewFallbackAttemptRecorderWithCap(10)
	rec.Record(FallbackAttempt{Provider: "a", Model: "m", ConnectionID: "c1", StatusCode: 503, Error: "503", LatencyMs: 20})
	rec.Record(FallbackAttempt{Provider: "a", Model: "m", ConnectionID: "c2", StatusCode: 429, Error: "429", LatencyMs: 15})
	rec.Record(FallbackAttempt{Provider: "a", Model: "m", ConnectionID: "c3", StatusCode: 200, LatencyMs: 90})
	RoutingExplainFromRecorder(rec)

	entries := RoutingExplainSnapshot(100)
	if len(entries) < 3 {
		t.Fatalf("ring holds %d entries, want at least the 3 chain steps", len(entries))
	}
	last3 := entries[len(entries)-3:]
	if last3[0].Outcome != "attempted" || last3[0].Provider != "a" || last3[0].ConnectionID != "c1" {
		t.Errorf("step 1 = %+v, want the first failed attempt", last3[0])
	}
	if last3[1].StatusCode != 429 || !strings.Contains(last3[1].Reason, "rate limited") {
		t.Errorf("step 2 = %+v, want the 429 reason", last3[1])
	}
	if last3[2].Outcome != "success" {
		t.Errorf("step 3 = %+v, want the success", last3[2])
	}
}

// TestRoutingExplainRingBounded pins the retention: the ring caps at 200
// entries and drops the oldest.
func TestRoutingExplainRingBounded(t *testing.T) {
	for i := 0; i < 250; i++ {
		RecordRoutingExplain(RoutingExplain{Provider: "p", Model: "m", Reason: "test", Outcome: "attempted"})
	}
	entries := RoutingExplainSnapshot(100)
	if len(entries) != 100 {
		t.Fatalf("snapshot limit = %d entries, want 100", len(entries))
	}
}
