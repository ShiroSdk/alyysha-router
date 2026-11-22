// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

import (
	"strings"
	"testing"

	"alyysha/proxy/internal/models"
)

func TestModelFamily(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gpt-4.1", "gpt"},
		{"gpt-4.1-mini", "gpt-mini"},
		{"claude-sonnet-4.5", "claude-sonnet"},
		{"claude-sonnet-4.6", "claude-sonnet"},
		{"gemini-3.1-flash-preview", "gemini-flash"},
		{"deepseek-chat", "deepseek-chat"},
		{"model:free", "model"},
		{"o3-mini", "o3-mini"},
	}
	for _, tc := range cases {
		if got := models.ModelFamily(tc.in); got != tc.want {
			t.Errorf("ModelFamily(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestComboFamilies(t *testing.T) {
	// Two size classes of one line: distinct families, but the same line, so
	// the cross flag stays off — the tokenizer and cache ride the line.
	fam, cross := models.ComboFamilies([]string{"openai/gpt-4.1", "openai/gpt-4.1-mini"})
	if cross || len(fam) != 2 || fam[0] != "gpt" || fam[1] != "gpt-mini" {
		t.Errorf("same-line ladder: %v cross=%v", fam, cross)
	}
	fam, cross = models.ComboFamilies([]string{"openai/gpt-4.1", "anthropic/claude-sonnet-4.5", "google/gemini-2.5-flash"})
	if !cross || len(fam) != 3 {
		t.Errorf("mixed ladder: %v cross=%v", fam, cross)
	}
	// A nested combo name has no family of its own.
	fam, cross = models.ComboFamilies([]string{"openai/gpt-4.1", "nested-combo-name"})
	if cross {
		t.Errorf("nested combo counted as a family: %v", fam)
	}
}

func TestRecordFamilyBoundaries(t *testing.T) {
	// The explain ring is shared with every other test in this package, so
	// the assertions look for THIS test's own records by their combo name
	// instead of counting entries.
	recordFamilyBoundaries("mixed-n9", []string{"openai/gpt-4.1", "anthropic/claude-sonnet-4.5", "google/gemini-2.5-flash"})
	snap := RoutingExplainSnapshot(200)
	var order []string
	for _, e := range snap {
		if e.Model == "mixed-n9" && strings.Contains(e.Reason, "crosses model family") {
			order = append(order, e.Reason[strings.Index(e.Reason, ":")+2:])
		}
	}
	if len(order) != 2 {
		t.Fatalf("boundary records = %d, want 2: %v", len(order), order)
	}
	if order[0] != "gpt -> claude" || order[1] != "claude -> gemini" {
		t.Errorf("boundary order = %v", order)
	}

	// A single-line ladder records nothing for its own name.
	recordFamilyBoundaries("plain-n9", []string{"openai/gpt-4.1", "openai/gpt-4.1-mini"})
	for _, e := range RoutingExplainSnapshot(200) {
		if e.Model == "plain-n9" {
			t.Errorf("same-line ladder recorded a boundary: %s", e.Reason)
		}
	}
}
