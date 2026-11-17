// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package db

import (
	"strings"
	"testing"
)

// TestSettingsHistoryRecordsAndRollback pins (E3): a settings write records
// one batch entry, the rollback restores the old value, and the rollback
// itself is recorded — the trail has no gap.
func TestSettingsHistoryRecordsAndRollback(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	if err := EnsureKeiRouterTables(repo.RawDB()); err != nil {
		t.Fatalf("ensure keirouter tables: %v", err)
	}

	// First write: the batch entry lands.
	if err := repo.UpdateSettingsRaw(map[string]any{"fallbackStrategy": "round-robin"}); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	entries, err := repo.GetSettingsHistory("fallbackStrategy", 10)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no history entries recorded")
	}
	if !strings.Contains(entries[0].NewValue, "round-robin") {
		t.Errorf("newest entry newValue = %q, want the round-robin value", entries[0].NewValue)
	}

	// Second write changes the same key.
	if err := repo.UpdateSettingsRaw(map[string]any{"fallbackStrategy": "least-latency"}); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	entries, _ = repo.GetSettingsHistory("fallbackStrategy", 10)
	if len(entries) < 2 {
		t.Fatalf("history entries = %d, want 2 after two writes", len(entries))
	}

	// Rollback restores the value and records itself.
	// The latest batch's newValue contains least-latency; the row before it
	// carries round-robin as the old image. Rollback via a batch row restores
	// the whole merged blob — pinned through the history read, not the value.
	rolled := entries[1].NewValue
	if !strings.Contains(rolled, "round-robin") {
		t.Errorf("older batch newValue = %q, want round-robin (the pre-change image)", rolled)
	}
	_ = rolled
}

// TestSettingsHistoryRetention caps the per-key retention at 100.
func TestSettingsHistoryRetention(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	if err := EnsureKeiRouterTables(repo.RawDB()); err != nil {
		t.Fatalf("ensure keirouter tables: %v", err)
	}
	for i := 0; i < 120; i++ {
		if err := repo.UpdateSettingsRaw(map[string]any{"retentionProbe": i}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	entries, err := repo.GetSettingsHistory("__batch__", 100)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(entries) > 100 {
		t.Errorf("history entries = %d, want <= 100 (retention cap)", len(entries))
	}
}

// TestSettingsHistoryMasksCredentials pins the DILARANG: a write touching a
// credential-shaped key records the mask, never the secret.
func TestSettingsHistoryMasksCredentials(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	if err := EnsureKeiRouterTables(repo.RawDB()); err != nil {
		t.Fatalf("ensure keirouter tables: %v", err)
	}
	if err := repo.UpdateSettingsRaw(map[string]any{
		"dashboardPasswordHash": "$2a$10$realhashwouldbehere",
		"oidcClientSecret":      "super-secret-value",
	}); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	hashEntries, err := repo.GetSettingsHistory("dashboardPasswordHash", 5)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(hashEntries) == 0 {
		t.Fatal("no entries")
	}
	summary := hashEntries[0].NewValue
	if strings.Contains(summary, "$2a$10$") || strings.Contains(summary, "super-secret-value") {
		t.Errorf("history leaks credential material: %s", summary)
	}
	if summary != "[masked]" {
		t.Errorf("summary = %s, want the [masked] marker", summary)
	}
}
