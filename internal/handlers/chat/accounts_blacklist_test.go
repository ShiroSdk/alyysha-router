// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"testing"
	"time"
)

// TestAccountsBlacklistTTLConfig pins the contract: 0 (default) keeps the
// old behavior, a positive value activates the override window.
func TestAccountsBlacklistTTLConfig(t *testing.T) {
	t.Cleanup(func() { SetAccountsBlacklistTTL(0) })
	SetAccountsBlacklistTTL(0)
	if accountsBlacklistTTLActive() {
		t.Error("default must be inactive")
	}
	SetAccountsBlacklistTTL(30)
	if !accountsBlacklistTTLActive() {
		t.Error("positive minutes must activate the blacklist")
	}
	until := accountsBlacklistUntil(time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	want := time.Date(2026, 11, 18, 12, 30, 0, 0, time.UTC)
	if !until.Equal(want) {
		t.Errorf("blacklist until = %v, want %v (30 minutes)", until, want)
	}
}
