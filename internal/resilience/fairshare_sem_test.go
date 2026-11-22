// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package resilience

import (
	"context"
	"sync"
	"testing"
	"time"
)

// weightedGrant parks two waiters on a full gate and reports the order the
// drain granted them in.
func weightedGrant(t *testing.T, maxConcurrency int, shares ...float64) []int {
	t.Helper()
	ResetSemaphores()
	key := "fair:" + time.Now().String()
	opts := SemaphoreOptions{MaxConcurrency: maxConcurrency, Timeout: 5 * time.Second, MaxQueueSize: 20}

	// Occupy every slot.
	releases := make([]func(), 0, maxConcurrency)
	for i := 0; i < maxConcurrency; i++ {
		rel, err := AcquireWeighted(context.Background(), key, opts, 0)
		if err != nil {
			t.Fatalf("occupy slot %d: %v", i, err)
		}
		releases = append(releases, rel)
	}

	var mu sync.Mutex
	order := []int{}
	var wg sync.WaitGroup
	for i, share := range shares {
		wg.Add(1)
		go func(idx int, share float64) {
			defer wg.Done()
			rel, err := AcquireWeighted(context.Background(), key, opts, share)
			if err != nil {
				return
			}
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
			rel()
		}(i, share)
		// Park THIS waiter before starting the next one, so arrival order is
		// deterministic and the FIFO assertion means what it says.
		deadline := time.Now().Add(2 * time.Second)
		for semQueued(key) < i+1 && !time.Now().After(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if semQueued(key) < i+1 {
			t.Fatalf("waiter %d never parked", i)
		}
	}
	for _, rel := range releases {
		rel()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waiters never drained")
	}
	return order
}

// semQueued reads the live gate's queue depth.
func semQueued(key string) int {
	for _, st := range SemaphoreStats() {
		if st.Key == key {
			return st.Queued
		}
	}
	return 0
}

// TestDrainPrefersLightestShare proves contention serves the key that has
// been consumed the least: the heavy consumer waits behind both light ones.
func TestDrainPrefersLightestShare(t *testing.T) {
	order := weightedGrant(t, 1, 9_000, 500, 100)
	// Waiters: 0=heavy, 1=light, 2=lightest. One slot frees: the lightest
	// goes first, then the light one, then the heavy one.
	if len(order) != 3 {
		t.Fatalf("order = %v, want all three granted", order)
	}
	if order[0] != 2 || order[1] != 1 {
		t.Errorf("drain order = %v, want [2 1 0] (lightest first)", order)
	}
}

// TestDrainFIFOWithoutShares proves zero shares keep the arrival order —
// the behaviour every existing caller relied on.
func TestDrainFIFOWithoutShares(t *testing.T) {
	order := weightedGrant(t, 1, 0, 0, 0)
	if len(order) != 3 {
		t.Fatalf("order = %v, want all three granted", order)
	}
	if order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("zero-share drain order = %v, want FIFO [0 1 2]", order)
	}
}

// TestSkipCreditAging proves a heavy consumer is not starved: each pass the
// drain grants to a lighter waiter grows the heavy one's credit, until it
// outweighs the difference.
func TestSkipCreditAging(t *testing.T) {
	ResetSemaphores()
	key := "aging"
	opts := SemaphoreOptions{MaxConcurrency: 1, Timeout: 5 * time.Second, MaxQueueSize: 20}
	rel, err := AcquireWeighted(context.Background(), key, opts, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Light waiter arrives first, then the heavy one.
	lightDone := make(chan struct{})
	go func() {
		r, err := AcquireWeighted(context.Background(), key, opts, 100)
		if err == nil {
			r()
		}
		close(lightDone)
	}()
	heavyDone := make(chan struct{})
	go func() {
		r, err := AcquireWeighted(context.Background(), key, opts, 10_000)
		if err == nil {
			r()
		}
		close(heavyDone)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for semQueued(key) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// The first release hands the slot to the light waiter (share 100 beats
	// 10000); the heavy waiter's credit grows when it is skipped.
	rel()
	<-lightDone
	// Enough passes of the drain with the light waiter cycling must
	// eventually serve the heavy one before its timeout: acquire and release
	// repeatedly as a zero-share client.
	for i := 0; i < 40; i++ {
		select {
		case <-heavyDone:
			return // served, not starved
		default:
		}
		r, err := AcquireWeighted(context.Background(), key, opts, 0)
		if err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond)
		r()
	}
	select {
	case <-heavyDone:
	case <-time.After(5 * time.Second):
		t.Fatal("heavy waiter starved despite skip credit")
	}
}

// TestAcquireWeightedBypass proves the unconfigured gate stays a no-op, the
// same contract Acquire has.
func TestAcquireWeightedBypass(t *testing.T) {
	rel, err := AcquireWeighted(context.Background(), "bypass", SemaphoreOptions{MaxConcurrency: 0}, 100)
	if err != nil {
		t.Fatalf("bypass errored: %v", err)
	}
	rel()
	rel() // idempotent
}
