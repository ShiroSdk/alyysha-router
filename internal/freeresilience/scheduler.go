// Alyysha Router — ported & modified by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package freeresilience

import (
	"context"
	"log"
	"time"
)

// DefaultSchedulerInterval is how often the background pass expires passed
// cooldowns and flushes health outcomes to SQLite. One minute bounds how
// stale the dashboard's cooldown countdown can look while costing one tiny
// indexed write per provider with traffic.
const DefaultSchedulerInterval = time.Minute

// StartScheduler runs the recovery background pass until the context ends:
// every tick it lifts cooldowns whose reset has passed (the provider rejoins
// rotation without a restart) and persists the health outcome rings. It
// follows the same shape as db.StartFreeTierResetLoop and the other
// fire-and-forget background jobs.
func StartScheduler(ctx context.Context, tracker *Tracker, interval time.Duration) {
	if tracker == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultSchedulerInterval
	}
	go func() {
		// An initial pass right away: cooldowns loaded from the previous run
		// that expired while the process was down are lifted at boot, not a
		// minute later.
		tracker.ExpireCooldowns()
		tracker.FlushHealth()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if rejoined := tracker.ExpireCooldowns(); len(rejoined) > 0 {
					log.Printf("[freeresilience] %d provider(s) rejoined rotation: %v", len(rejoined), rejoined)
					// The recovery ride-along: the operator's configured order
					// already prefers these providers again on the next
					// request, and a fresh failure re-cools them with a
					// longer window (the attempts counter scales it). The
					// notification makes the recovery visible.
					if notifier, ok := tracker.Store().(RecoveryNotifier); ok {
						created := NotifyRecovery(ctx, notifier, rejoined, time.Now())
						log.Printf("[freeresilience] %d recovery notification(s) created", len(created))
					}
				}
				tracker.FlushHealth()
				tracker.FlushRadar()
			}
		}
	}()
}
