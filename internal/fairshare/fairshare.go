// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
// Package fairshare keeps a rolling per-client-key consumption ledger so
// admission under contention can favor the keys that have been served the
// least (N4).
//
// The ledger is deliberately in-memory and coarse: twelve buckets of five
// minutes, each holding the tokens a key was served in that slice. A key's
// share is the window sum. That is enough to answer "who has been eating the
// pool lately" without a database read on the hot path, and a restart simply
// forgets the shares — fairness is a scheduling hint, not a ledger anyone
// bills from.
//
// Identity is the masked client key (db.LedgerMask), the same spelling the
// usage ledger attributes rows by, so the writer (the usage logger) and the
// reader (the admission gate) agree on the key without sharing anything else.
package fairshare

import (
	"sync"
	"time"
)

const (
	bucketCount = 12
	bucketSpan  = 5 * time.Minute
	// maxKeys bounds the ledger. Past it, the stale half of the map is
	// dropped wholesale: this is a scheduling hint, and a deployment serving
	// thousands of distinct keys in an hour prefers cheap eviction over
	// unbounded growth.
	maxKeys = 4096
)

type ledger struct {
	mu      sync.Mutex
	buckets []int64
	touched time.Time
}

var (
	storeMu sync.Mutex
	store   = map[string]*ledger{}
	clock   = time.Now // test hook
)

// Add credits tokens to the key's current bucket.
func Add(clientKey string, tokens int64) {
	if clientKey == "" || tokens <= 0 {
		return
	}
	now := clock()
	l := ledgerFor(clientKey, now)
	l.mu.Lock()
	l.advance(now)
	l.buckets[currentBucket(now)] += tokens
	l.mu.Unlock()
	maybeEvict(now)
}

// Share is the key's consumption across the window: the sum of its buckets.
// An unknown key shares zero, which is exactly the "served the least" weight
// a fresh key should carry.
func Share(clientKey string) float64 {
	if clientKey == "" {
		return 0
	}
	now := clock()
	storeMu.Lock()
	l, ok := store[clientKey]
	storeMu.Unlock()
	if !ok {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.advance(now)
	var total int64
	for _, v := range l.buckets {
		total += v
	}
	return float64(total)
}

// Reset clears the ledger. Used by tests.
func Reset() {
	storeMu.Lock()
	store = map[string]*ledger{}
	storeMu.Unlock()
}

func ledgerFor(clientKey string, now time.Time) *ledger {
	storeMu.Lock()
	defer storeMu.Unlock()
	if l, ok := store[clientKey]; ok {
		return l
	}
	l := &ledger{buckets: make([]int64, bucketCount), touched: now}
	store[clientKey] = l
	return l
}

// currentBucket indexes the slice the clock is in. The mapping only needs to
// be stable within one process: advance() clears buckets the clock has left,
// so a stale index never survives into a new span.
func currentBucket(now time.Time) int {
	return int(now.Unix()/int64(bucketSpan/time.Second)) % bucketCount
}

// advance zeroes every bucket the clock has rolled past since the last touch.
// A bucket the clock is currently in is kept; a key silent for the whole
// window reads as zero share.
func (l *ledger) advance(now time.Time) {
	cur := currentBucket(now)
	if l.touched.IsZero() {
		l.touched = now
		return
	}
	prev := currentBucket(l.touched)
	if prev == cur && now.Sub(l.touched) < bucketSpan {
		return
	}
	for i := range l.buckets {
		l.buckets[i] = 0
	}
	l.touched = now
}

// maybeEvict drops the whole ledger when it grows past the cap. A map this
// size is far beyond a personal gateway's key count; when it happens, every
// key starts from zero share again, which is the same state a restart leaves.
func maybeEvict(now time.Time) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if len(store) <= maxKeys {
		return
	}
	for k, l := range store {
		l.mu.Lock()
		stale := now.Sub(l.touched) > bucketSpan
		l.mu.Unlock()
		if stale {
			delete(store, k)
		}
	}
	if len(store) > maxKeys {
		// Still over after the stale sweep: drop everything rather than grow.
		store = map[string]*ledger{}
	}
}
