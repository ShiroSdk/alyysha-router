// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestRetryDedupSecondIdenticalRequestSkipsLadder pins (a): two requests
// with identical bodies that both answer 503 — the first pays the retry
// ladder, the second skips it because the body is remembered as recently
// failed.
func TestRetryDedupSecondIdenticalRequestSkipsLadder(t *testing.T) {
	prevDelays := transientRetryPolicy[http.StatusServiceUnavailable]
	prevClock := retryDedupClock
	transientRetryPolicy[http.StatusServiceUnavailable] = transientRetry{attempts: 3, delay: time.Millisecond}
	retryDedupClock = func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		transientRetryPolicy[http.StatusServiceUnavailable] = prevDelays
		retryDedupClock = prevClock
		retryBodyMu.Lock()
		retryBodySeen = map[string]retryBodyRecord{}
		retryBodyOrder = nil
		retryBodyMu.Unlock()
	})

	var hits atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer site.Close()

	const body = `{"model":"x","messages":[{"role":"user","content":"same"}]}`
	// The call ends in the 503 the fake upstream answers with; the assertion
	// is on how many upstream hits the ladder spent, not on the error.
	first, _ := DoRequest(context.Background(), http.DefaultClient, http.MethodPost, site.URL,
		map[string]string{"Content-Type": "application/json"}, []byte(body))
	if first != nil {
		_ = first.Body.Close()
	}
	if got := hits.Load(); got != 4 { // 1 + 3 retries
		t.Fatalf("first request upstream hits = %d, want 4 (ladder ran)", got)
	}

	second, _ := DoRequest(context.Background(), http.DefaultClient, http.MethodPost, site.URL,
		map[string]string{"Content-Type": "application/json"}, []byte(body))
	if second != nil {
		_ = second.Body.Close()
	}
	if got := hits.Load(); got != 5 { // 1 attempt only — ladder skipped
		t.Fatalf("second request upstream hits = %d, want 5 total (ladder skipped)", got)
	}
}

// TestRetryDedupTTLE XPires: after the TTL the ladder runs again.
func TestRetryDedupTTLExpires(t *testing.T) {
	prevDelays := transientRetryPolicy[http.StatusServiceUnavailable]
	prevClock := retryDedupClock
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	transientRetryPolicy[http.StatusServiceUnavailable] = transientRetry{attempts: 1, delay: time.Millisecond}
	retryDedupClock = func() time.Time { return base }
	t.Cleanup(func() {
		transientRetryPolicy[http.StatusServiceUnavailable] = prevDelays
		retryDedupClock = prevClock
		retryBodyMu.Lock()
		retryBodySeen = map[string]retryBodyRecord{}
		retryBodyOrder = nil
		retryBodyMu.Unlock()
	})

	var hits atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer site.Close()

	const body = `{"same":"body"}`
	req1, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, site.URL, nil)
	_ = req1
	_, _ = DoRequest(context.Background(), http.DefaultClient, http.MethodPost, site.URL,
		map[string]string{}, []byte(body))
	before := hits.Load()

	// Advance the fake clock past the TTL.
	retryDedupClock = func() time.Time { return base.Add(retryBodyTTL + time.Minute) }
	_, _ = DoRequest(context.Background(), http.DefaultClient, http.MethodPost, site.URL,
		map[string]string{}, []byte(body))
	after := hits.Load()

	if after-before < 2 { // 1 attempt + 1 retry — the ladder ran again
		t.Fatalf("hits after TTL = %d (before %d): ladder must run again once the TTL expires", after, before)
	}
}

// TestRetryBodyAttemptsSpent pins the attempt accounting the chat fallback
// caps read.
func TestRetryBodyAttemptsSpent(t *testing.T) {
	prevClock := retryDedupClock
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	retryDedupClock = func() time.Time { return base }
	t.Cleanup(func() {
		retryDedupClock = prevClock
		retryBodyMu.Lock()
		retryBodySeen = map[string]retryBodyRecord{}
		retryBodyOrder = nil
		retryBodyMu.Unlock()
	})

	body := []byte(`{"attempts":"count"}`)
	RecordBodyAttempts(body, "deepseek", 3)
	RecordBodyAttempts(body, "deepseek", 2)
	RecordBodyAttempts(body, "glm", 1)
	if got := BodyAttemptsSpent(body); got != 6 {
		t.Errorf("BodyAttemptsSpent = %d, want 6", got)
	}
	if got := BodyProviderAttemptsSpent(body, "deepseek"); got != 5 {
		t.Errorf("BodyProviderAttemptsSpent(deepseek) = %d, want 5", got)
	}
	if got := BodyProviderAttemptsSpent(body, "glm"); got != 1 {
		t.Errorf("BodyProviderAttemptsSpent(glm) = %d, want 1", got)
	}
}
