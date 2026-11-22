// Alyysha Router — derived from the work of https://github.com/ShiroSdk/ — port and modifications by the maintainers — https://t.me/YannClippers
package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Semaphore is a FIFO concurrency limiter keyed by provider:account:proxy.
//
// A request beyond the cap waits for a slot instead of failing, up to a timeout;
// the queue is bounded so a burst cannot grow without limit. This mirrors
// VansRouter's open-sse/services/accountSemaphore.js and exists to stop one
// account from being hit with more concurrent requests than it can serve, which
// is how a 429 cascade starts.
//
// A gate can also be blocked outright (Block) when the account is known to be
// rate-limited, so queued waiters are held until the block expires.

// ErrSemaphoreCapacity means the queue was full or the wait timed out.
var ErrSemaphoreCapacity = errors.New("semaphore capacity reached")

// SemaphoreOptions configures a gate. Zero values take the defaults.
type SemaphoreOptions struct {
	MaxConcurrency int
	Timeout        time.Duration
	MaxQueueSize   int
}

const (
	defaultMaxConcurrency = 3
	defaultTimeout        = 30 * time.Second
	defaultMaxQueueSize   = 20
)

// SemaphoreKey builds the gate key. proxyHash separates accounts that share a
// proxy so one dead proxy's limit does not throttle another's.
func SemaphoreKey(provider, accountKey, proxyHash string) string {
	if proxyHash == "" {
		proxyHash = "direct"
	}
	return provider + ":" + accountKey + ":" + proxyHash
}

type waiter struct {
	ready chan func()
	// share is the client key's recent consumption (N4 fair-share). Zero —
	// the default for every unweighted caller — means "served the least", so
	// a semaphore whose waiters carry no shares drains in exactly the FIFO
	// order it always did.
	share float64
	// seq is the arrival sequence, the deterministic tie-break that makes
	// equal shares drain FIFO.
	seq uint64
	// credit grows each time a drain passes this waiter over for a lighter
	// one, and is subtracted from its share when the next slot is handed out.
	// Without it a heavy consumer that keeps losing to newly-arrived light
	// consumers could starve past its timeout; with it every skipped waiter
	// eventually wins a slot.
	credit float64
}

// semaphoreSeq stamps arrival order for the tie-break.
var semaphoreSeq atomic.Uint64

// skipCredit is the share-worth of tokens a waiter is forgiven each time a
// drain passes it over. Roughly one skipped slot buys back a thousand-token
// request's share; the exact scale only needs to make aging outrun the
// arrival rate of lighter waiters.
const skipCredit = 1000

type gate struct {
	mu             sync.Mutex
	running        int
	maxConcurrency int
	queue          []*waiter
	blockedUntil   time.Time
}

// registry holds one gate per key.
var (
	semMu    sync.Mutex
	semGates = map[string]*gate{}
)

func gateFor(key string, maxConcurrency int) *gate {
	semMu.Lock()
	defer semMu.Unlock()
	if g, ok := semGates[key]; ok {
		g.mu.Lock()
		g.maxConcurrency = maxConcurrency
		g.mu.Unlock()
		return g
	}
	g := &gate{maxConcurrency: maxConcurrency}
	semGates[key] = g
	return g
}

// cleanupGate drops an idle gate so the registry does not grow forever. A gate
// with waiters or a live block is kept.
func cleanupGate(key string, g *gate) {
	g.mu.Lock()
	idle := g.running == 0 && len(g.queue) == 0 &&
		(g.blockedUntil.IsZero() || time.Now().After(g.blockedUntil))
	g.mu.Unlock()
	if !idle {
		return
	}
	semMu.Lock()
	// Re-check under the registry lock: a concurrent acquire may have taken it.
	g.mu.Lock()
	stillIdle := g.running == 0 && len(g.queue) == 0 &&
		(g.blockedUntil.IsZero() || time.Now().After(g.blockedUntil))
	g.mu.Unlock()
	if stillIdle && semGates[key] == g {
		delete(semGates, key)
	}
	semMu.Unlock()
}

// Acquire reserves a slot and returns the release function. The caller MUST
// call release exactly once (it is safe to call more than once).
//
// A MaxConcurrency of <= 0 bypasses the limiter entirely, returning a no-op
// release.
func Acquire(ctx context.Context, key string, opts SemaphoreOptions) (func(), error) {
	if opts.MaxConcurrency <= 0 {
		return func() {}, nil
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxQueueSize <= 0 {
		opts.MaxQueueSize = defaultMaxQueueSize
	}
	g := gateFor(key, opts.MaxConcurrency)
	g.mu.Lock()
	// Fast path: a free slot and no active block.
	blocked := !g.blockedUntil.IsZero() && time.Now().Before(g.blockedUntil)
	if !blocked && g.running < g.maxConcurrency {
		g.running++
		g.mu.Unlock()
		return makeRelease(key, g), nil
	}
	// A full queue is refused outright rather than waiting for a timeout.
	if len(g.queue) >= opts.MaxQueueSize {
		g.mu.Unlock()
		return nil, fmt.Errorf("%w: queue full for %q", ErrSemaphoreCapacity, key)
	}
	w := &waiter{ready: make(chan func(), 1), seq: semaphoreSeq.Add(1)}
	g.queue = append(g.queue, w)
	g.mu.Unlock()

	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()
	select {
	case release := <-w.ready:
		return release, nil
	case <-timer.C:
		if g.removeWaiter(w) {
			return nil, fmt.Errorf("%w: timed out after %s for %q", ErrSemaphoreCapacity, opts.Timeout, key)
		}
		// The slot was granted in the same instant the timer fired: honour it
		// rather than leaking a reservation.
		return <-w.ready, nil
	case <-ctx.Done():
		if g.removeWaiter(w) {
			return nil, ctx.Err()
		}
		return <-w.ready, nil
	}
}

// AcquireWeighted reserves a slot on behalf of a client key whose recent
// consumption is share (N4). Under contention the freed slot goes to the
// waiter that has been served the least, so a burst from one heavy key
// cannot hold every slot while a light key waits behind it. With no shares
// in play the drain is the same FIFO Acquire has always run.
func AcquireWeighted(ctx context.Context, key string, opts SemaphoreOptions, share float64) (func(), error) {
	if opts.MaxConcurrency <= 0 {
		return func() {}, nil
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	g := gateFor(key, opts.MaxConcurrency)
	// The fast path and queue-full checks must behave exactly like Acquire;
	// only the drain order differs. Take the gate lock once here and fall
	// through to the same park the unweighted path uses.
	g.mu.Lock()
	blocked := !g.blockedUntil.IsZero() && time.Now().Before(g.blockedUntil)
	if !blocked && g.running < g.maxConcurrency {
		g.running++
		g.mu.Unlock()
		return makeRelease(key, g), nil
	}
	if opts.MaxQueueSize <= 0 {
		opts.MaxQueueSize = defaultMaxQueueSize
	}
	if len(g.queue) >= opts.MaxQueueSize {
		g.mu.Unlock()
		return nil, fmt.Errorf("%w: queue full for %q", ErrSemaphoreCapacity, key)
	}
	w := &waiter{ready: make(chan func(), 1), share: share, seq: semaphoreSeq.Add(1)}
	g.queue = append(g.queue, w)
	g.mu.Unlock()

	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()
	select {
	case release := <-w.ready:
		return release, nil
	case <-timer.C:
		if g.removeWaiter(w) {
			return nil, fmt.Errorf("%w: timed out after %s for %q", ErrSemaphoreCapacity, opts.Timeout, key)
		}
		return <-w.ready, nil
	case <-ctx.Done():
		if g.removeWaiter(w) {
			return nil, ctx.Err()
		}
		return <-w.ready, nil
	}
}

// removeWaiter takes w out of the queue, reporting whether it was still there.
func (g *gate) removeWaiter(w *waiter) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, queued := range g.queue {
		if queued == w {
			g.queue = append(g.queue[:i], g.queue[i+1:]...)
			return true
		}
	}
	return false
}

// makeRelease returns the idempotent release function for a granted slot.
func makeRelease(key string, g *gate) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.running--
			g.mu.Unlock()
			drain(key, g)
			cleanupGate(key, g)
		})
	}
}

// drain hands free slots to queued waiters, stopping at an active block.
func drain(key string, g *gate) {
	for {
		g.mu.Lock()
		if len(g.queue) == 0 || g.running >= g.maxConcurrency {
			g.mu.Unlock()
			return
		}
		if !g.blockedUntil.IsZero() && time.Now().Before(g.blockedUntil) {
			g.mu.Unlock()
			return
		}
		// Fair-share selection (N4): under contention the slot goes to the
		// waiter that has been served the least, aged by the credits it has
		// earned waiting. Zero shares make the minimum the earliest arrival,
		// which is the FIFO order this queue has always had — so a deployment
		// that never weighted a waiter behaves byte for byte as before.
		best := 0
		if len(g.queue) > 1 {
			bestShare := g.queue[0].share - g.queue[0].credit
			for i := 1; i < len(g.queue); i++ {
				s := g.queue[i].share - g.queue[i].credit
				if s < bestShare || (s == bestShare && g.queue[i].seq < g.queue[best].seq) {
					best = i
				}
			}
		}
		for i := range g.queue {
			if i != best {
				g.queue[i].credit += skipCredit
			}
		}
		w := g.queue[best]
		g.queue = append(g.queue[:best], g.queue[best+1:]...)
		g.running++
		g.mu.Unlock()
		// A waiter that timed out removes itself from the queue under the same
		// lock, so anything still queued here is live and must receive its
		// slot — otherwise the reservation would leak.
		w.ready <- makeRelease(key, g)
	}
}

// Block holds every request on a gate for the given duration. Used when an
// account returns 429 so queued and future requests wait instead of piling on.
//
// The gate is created if it does not exist yet, so a 429 that arrives before
// the first acquire still takes effect.
func Block(key string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	g := gateFor(key, defaultMaxConcurrency)
	until := time.Now().Add(duration)
	g.mu.Lock()
	if g.blockedUntil.Before(until) {
		g.blockedUntil = until
	}
	g.mu.Unlock()
	// Wake up when the block expires so queued waiters are not stuck until the
	// next Acquire call.
	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		<-timer.C
		drain(key, g)
		cleanupGate(key, g)
	}()
}

// SemaphoreStat is a snapshot of one gate, for the dashboard.
type SemaphoreStat struct {
	Key            string `json:"key"`
	Running        int    `json:"running"`
	Queued         int    `json:"queued"`
	MaxConcurrency int    `json:"maxConcurrency"`
	BlockedUntil   string `json:"blockedUntil,omitempty"`
}

// SemaphoreStats returns a snapshot of every live gate.
func SemaphoreStats() []SemaphoreStat {
	semMu.Lock()
	gates := make(map[string]*gate, len(semGates))
	for k, g := range semGates {
		gates[k] = g
	}
	semMu.Unlock()
	out := make([]SemaphoreStat, 0, len(gates))
	for k, g := range gates {
		g.mu.Lock()
		st := SemaphoreStat{
			Key:            k,
			Running:        g.running,
			Queued:         len(g.queue),
			MaxConcurrency: g.maxConcurrency,
		}
		if !g.blockedUntil.IsZero() && time.Now().Before(g.blockedUntil) {
			st.BlockedUntil = g.blockedUntil.UTC().Format(time.RFC3339)
		}
		g.mu.Unlock()
		out = append(out, st)
	}
	return out
}

// ResetSemaphores clears every gate. Used by tests.
func ResetSemaphores() {
	semMu.Lock()
	semGates = map[string]*gate{}
	semMu.Unlock()
}
