// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Retry-body memory: a small LRU of recently failed transient bodies. Two
// clients sending the same body against an upstream that just answered 503
// should not both pay the full retry ladder — the second one skips straight
// to the fallback chain. The same store counts fallback attempts per body
// (and per provider) so the configured attempt caps can cut a retrying
// request before it spends the whole budget again.
//
// In-memory only, bounded at 512 entries with a 5-minute TTL: this is a
// dedup hint, not a ledger.

const (
	retryBodyMaxEntries = 512
	retryBodyTTL        = 5 * time.Minute
)

type retryBodyRecord struct {
	failedAt  time.Time
	attempts  int
	providers map[string]int
}

var (
	retryBodyMu     sync.Mutex
	retryBodySeen   = map[string]retryBodyRecord{}
	retryBodyOrder  []string // FIFO of hashes for LRU eviction
	retryDedupClock = time.Now
)

// RetryBodyHash is the SHA-256 of a request body — the dedup key.
func RetryBodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func retryBodyEvictLocked(hash string) {
	if len(retryBodySeen) < retryBodyMaxEntries {
		return
	}
	for len(retryBodySeen) >= retryBodyMaxEntries && len(retryBodyOrder) > 0 {
		oldest := retryBodyOrder[0]
		retryBodyOrder = retryBodyOrder[1:]
		delete(retryBodySeen, oldest)
	}
	retryBodyOrder = append(retryBodyOrder, hash)
}

// RecordTransientBodyFailure marks a body as having just failed a transient
// upstream status, so the next request with the same body skips its retries.
func RecordTransientBodyFailure(body []byte) {
	hash := RetryBodyHash(body)
	retryBodyMu.Lock()
	defer retryBodyMu.Unlock()
	now := retryDedupClock()
	rec, ok := retryBodySeen[hash]
	if !ok {
		retryBodyEvictLocked(hash)
		rec = retryBodyRecord{providers: map[string]int{}}
	}
	rec.failedAt = now
	retryBodySeen[hash] = rec
}

// BodyRecentlyFailed reports whether this body failed transiently within the
// TTL, with the reason for the skip log.
func BodyRecentlyFailed(body []byte) (string, bool) {
	hash := RetryBodyHash(body)
	retryBodyMu.Lock()
	defer retryBodyMu.Unlock()
	rec, ok := retryBodySeen[hash]
	if !ok {
		return "", false
	}
	if retryDedupClock().Sub(rec.failedAt) > retryBodyTTL {
		return "", false
	}
	return "same body failed transiently " + retryBodyTTL.String() + " ago; retry ladder skipped", true
}

// RecordBodyAttempts adds n spent fallback attempts for a body (and its
// provider, when one is known) to the attempt-cap accounting.
func RecordBodyAttempts(body []byte, provider string, n int) {
	if n <= 0 {
		return
	}
	hash := RetryBodyHash(body)
	retryBodyMu.Lock()
	defer retryBodyMu.Unlock()
	now := retryDedupClock()
	rec, ok := retryBodySeen[hash]
	if !ok {
		retryBodyEvictLocked(hash)
		rec = retryBodyRecord{providers: map[string]int{}, failedAt: now}
	}
	rec.attempts += n
	if provider != "" {
		rec.providers[provider] += n
	}
	retryBodySeen[hash] = rec
}

// BodyAttemptsSpent is the cross-request attempt count a body already spent.
func BodyAttemptsSpent(body []byte) int {
	hash := RetryBodyHash(body)
	retryBodyMu.Lock()
	defer retryBodyMu.Unlock()
	rec, ok := retryBodySeen[hash]
	if !ok || retryDedupClock().Sub(rec.failedAt) > retryBodyTTL {
		return 0
	}
	return rec.attempts
}

// BodyProviderAttemptsSpent is the cross-request attempt count one provider
// already spent for this body.
func BodyProviderAttemptsSpent(body []byte, provider string) int {
	hash := RetryBodyHash(body)
	retryBodyMu.Lock()
	defer retryBodyMu.Unlock()
	rec, ok := retryBodySeen[hash]
	if !ok || retryDedupClock().Sub(rec.failedAt) > retryBodyTTL {
		return 0
	}
	return rec.providers[provider]
}

type retryBodyHashCtxKey struct{}

// WithRetryBodyHash attaches the body hash to the request context so the
// retry ladder can consult the dedup store.
func WithRetryBodyHash(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, retryBodyHashCtxKey{}, hash)
}

// RetryBodyHashFrom returns the hash attached by DoRequest, if any.
func RetryBodyHashFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	hash, ok := ctx.Value(retryBodyHashCtxKey{}).(string)
	return hash, ok && hash != ""
}
