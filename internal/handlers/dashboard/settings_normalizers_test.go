// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"testing"
)

// The section normalizers must leave a valid section in the payload so
// UpdateSettingsRaw persists it: the boot-time restores in NewChatHandler
// read these same sections from the stored blob, and a normalizer that
// stripped its section turned every live-applied toggle into a setting that
// silently vanished on the next restart.
func TestSectionNormalizersKeepSectionForPersistence(t *testing.T) {
	cases := []struct {
		name       string
		normalizer func(map[string]any)
		section    string
	}{
		{"context_guard", applyContextGuardUpdate, "fallback"},
		{"breach_mode", applyBudgetBreachModeUpdate, "budget"},
		{"blacklist_ttl", applyAccountsBlacklistTTLUpdate, "accounts"},
		{"autoprune_ttl", applyKeysAutopruneTTLUpdate, "accounts"},
		{"session_affinity", applySessionAffinityUpdate, "routing"},
		{"attempt_caps", applyRetryAttemptCapsUpdate, "retry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			switch tc.section {
			case "fallback":
				updates := map[string]any{"fallback": map[string]any{"context_guard": "truncate"}}
				tc.normalizer(updates)
				assertSectionKept(t, updates, tc.section)
			case "budget":
				updates := map[string]any{"budget": map[string]any{"breach_mode": "downgrade"}}
				tc.normalizer(updates)
				assertSectionKept(t, updates, tc.section)
			case "accounts":
				updates := map[string]any{"accounts": map[string]any{"blacklist_ttl": 30.0, "autoprune_ttl": 60.0}}
				tc.normalizer(updates)
				assertSectionKept(t, updates, tc.section)
			case "routing":
				updates := map[string]any{"routing": map[string]any{"session_affinity": "combo"}}
				tc.normalizer(updates)
				assertSectionKept(t, updates, tc.section)
			case "retry":
				updates := map[string]any{"retry": map[string]any{"attempt_caps": map[string]any{"exact": 2.0, "provider": 3.0}}}
				tc.normalizer(updates)
				assertSectionKept(t, updates, tc.section)
			}
		})
	}
}

func assertSectionKept(t *testing.T, updates map[string]any, section string) {
	t.Helper()
	if _, ok := updates[section]; !ok {
		t.Fatalf("normalizer stripped section %q; the boot-time restore can never find it", section)
	}
}

// A payload that is not the expected shape is stripped entirely so garbage
// never reaches the store.
func TestSectionNormalizersStripMalformedPayload(t *testing.T) {
	updates := map[string]any{"budget": "not-a-map"}
	applyBudgetBreachModeUpdate(updates)
	if _, ok := updates["budget"]; ok {
		t.Fatal("malformed section survived the normalizer")
	}
}
