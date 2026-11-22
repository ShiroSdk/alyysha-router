// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package dashboard

import (
	"testing"

	"alyysha/proxy/internal/budget"
	"alyysha/proxy/internal/handlers/chat"
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
		{"tool_loop_watch", applyToolLoopWatchUpdate, "chat"},
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
			case "chat":
				updates := map[string]any{"chat": map[string]any{"tool_loop_watch": "break", "tool_loop_threshold": 5.0}}
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

// The full persistence path for the tool-loop settings: normalize, store,
// read back in the same shape the NewChatHandler restore walks.
func TestToolLoopSettingsRoundTrip(t *testing.T) {
	t.Cleanup(func() {
		chat.SetToolLoopWatch("off")
		chat.SetToolLoopThreshold(3)
		budget.SetBreachMode("reject")
	})
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	updates := map[string]any{"chat": map[string]any{"tool_loop_watch": "break", "tool_loop_threshold": 5.0}}
	applyToolLoopWatchUpdate(updates)
	if err := repo.UpdateSettingsRaw(updates); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	stored, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	chatRaw, ok := stored["chat"].(map[string]any)
	if !ok {
		t.Fatalf("chat section missing after round trip: %v", stored)
	}
	if chatRaw["tool_loop_watch"] != "break" {
		t.Errorf("tool_loop_watch = %v, want break", chatRaw["tool_loop_watch"])
	}
	if n, ok := chatRaw["tool_loop_threshold"].(float64); !ok || int(n) != 5 {
		t.Errorf("tool_loop_threshold = %v, want 5", chatRaw["tool_loop_threshold"])
	}
}
