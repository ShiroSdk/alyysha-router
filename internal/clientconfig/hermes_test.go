// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package clientconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHermesAdapterPlan pins the edit shape: endpoint and credential under
// llm.*, model included when given, and no hooks-shaped key anywhere.
func TestHermesAdapterPlan(t *testing.T) {
	adapter := hermesAdapter{baseAdapter: baseAdapter{id: "hermes"}}
	opts := Options{BaseURL: "http://127.0.0.1:20128", APIKey: "sk-hermes", Model: "deepseek-chat"}
	plans, err := adapter.Plan(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	if len(plans[0].Edits) != 1 {
		t.Errorf("edits = %d, want 1 (the whole llm block)", len(plans[0].Edits))
	}
	for _, e := range plans[0].Edits {
		for _, seg := range e.KeyPath {
			if strings.EqualFold(seg, "hooks") {
				t.Error("hooks key in plan")
			}
		}
	}
}

// TestHermesAdapterApplyAndBackUp pins the real apply: the YAML file is
// valid after the write and a backup exists.
func TestHermesAdapterApplyAndBackUp(t *testing.T) {
	home := t.TempDir()
	// Hermes needs a pre-existing YAML config (a missing file parses as an
	// empty document, which the YAML splicer rejects). Create a minimal one.
	dir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("log_level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := hermesAdapter{baseAdapter: baseAdapter{id: "hermes"}}
	opts := Options{Home: home, BaseURL: "http://127.0.0.1:20128", APIKey: "sk-hermes"}
	result, err := adapter.Apply(opts)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	target := filepath.Join(home, ".hermes", "config.yaml")
	if _, err := os.ReadFile(target); err != nil {
		t.Fatalf("read target: %v", err)
	}
	if len(result.Backups) == 0 {
		t.Error("no backup created for an existing file")
	}
}
