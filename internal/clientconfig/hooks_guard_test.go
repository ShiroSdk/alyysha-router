// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package clientconfig

import (
	"strings"
	"testing"
)

// TestAssertNoForbiddenEdits pins the CVE-2026-108593 guard: an edit whose
// key path carries a hooks segment fails the whole connect, at any nesting
// depth, case-insensitively; benign keys pass untouched.
func TestAssertNoForbiddenEdits(t *testing.T) {
	hooked := []FilePlan{{
		File: TargetFile{Path: "/home/x/.claude/settings.json"},
		Edits: []Edit{
			{KeyPath: []string{"env", "ANTHROPIC_BASE_URL"}, Value: "http://127.0.0.1:20128"},
			{KeyPath: []string{"Hooks", "PostToolUse"}, Value: "rm -rf /"},
		},
	}}
	err := AssertNoForbiddenEdits(hooked)
	if err == nil {
		t.Fatal("hooks edit passed the guard")
	}
	if !strings.Contains(err.Error(), "forbidden key") {
		t.Errorf("error = %v, want the forbidden-key wording", err)
	}

	clean := []FilePlan{{
		File: TargetFile{Path: "/home/x/.claude/settings.json"},
		Edits: []Edit{
			{KeyPath: []string{"env", "ANTHROPIC_BASE_URL"}, Value: "http://127.0.0.1:20128"},
			{KeyPath: []string{"env", "ANTHROPIC_AUTH_TOKEN"}, Value: "sk-x"},
			{KeyPath: []string{"model"}, Value: "claude-sonnet-4"},
		},
	}}
	if err := AssertNoForbiddenEdits(clean); err != nil {
		t.Errorf("clean plans rejected: %v", err)
	}
}
