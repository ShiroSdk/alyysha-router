// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"sync/atomic"
	"time"

	"9routerx/proxy/internal/log"
)

// accountsBlacklistTTLMins is how long a connection that answered 401/403/402
// stays blacklisted from rotation, in minutes. 0 (the default) keeps the old
// behavior — the classification cooldown alone; a positive value overrides
// the computed cooldown for those credential-failure statuses so a dead key
// is out of the pool for the operator's stated TTL.
var accountsBlacklistTTL = atomic.Int64{}

// SetAccountsBlacklistTTL applies the stored setting (minutes). Zero or
// negative restores the classification-driven cooldown.
func SetAccountsBlacklistTTL(minutes int) {
	if minutes > 0 {
		accountsBlacklistTTL.Store(int64(minutes))
		return
	}
	accountsBlacklistTTL.Store(0)
}

func accountsBlacklistTTLActive() bool {
	return accountsBlacklistTTL.Load() > 0
}

func accountsBlacklistUntil(now time.Time) time.Time {
	return now.Add(time.Duration(accountsBlacklistTTL.Load()) * time.Minute)
}

// keysAutopruneTTLMins is the auto-prune sweep interval input: connections
// deactivated for a credential failure longer than this are dropped from the
// active pool by the sweep. 0 (default) disables the sweep entirely.
var keysAutopruneTTLMins = atomic.Int64{}

// SetKeysAutopruneTTL applies the stored setting (minutes); 0 disables.
func SetKeysAutopruneTTL(minutes int) {
	if minutes > 0 {
		keysAutopruneTTLMins.Store(int64(minutes))
		return
	}
	keysAutopruneTTLMins.Store(0)
}

func keysAutopruneTTL() time.Duration {
	return time.Duration(keysAutopruneTTLMins.Load()) * time.Minute
}

// StartKeysAutopruneSweep runs the auto-prune background pass until ctx ends:
// each tick deactivates connections whose credential-failure marking is older
// than the configured TTL. A zero TTL (the default) keeps the sweep inert.
func StartKeysAutopruneSweep(ctx context.Context, h *ChatHandler, interval time.Duration) {
	if h == nil || h.Repo == nil {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ttl := keysAutopruneTTL()
				if ttl <= 0 {
					continue
				}
				cutoff := time.Now().UTC().Add(-ttl)
				ids, err := h.Repo.GetConnectionsDisabledBefore(cutoff)
				if err != nil {
					log.Warn("autoprune", "sweep read failed", "error", err)
					continue
				}
				for _, id := range ids {
					// Already deactivated by the marking; the sweep only logs
					// that the TTL released it from the pool for review.
					log.Info("autoprune", "connection past autoprune TTL; kept out of the active pool",
						"conn", id, "ttl_minutes", int(ttl.Minutes()))
				}
			}
		}
	}()
}
