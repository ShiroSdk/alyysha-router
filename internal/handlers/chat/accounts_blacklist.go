// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"sync/atomic"
	"time"
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
