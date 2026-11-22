// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

import (
	"testing"
	"time"

	"alyysha/proxy/internal/db"
)

// TestLKGPReordersByLeastUsage proves the ladder reorders toward the
// provider that has served the least in the window, keeping same-provider
// members in their stored order.
func TestLKGPReordersByLeastUsage(t *testing.T) {
	repo := setupLKGPRepo(t)
	ctx := t.Context()
	h := &ChatHandler{Repo: repo}

	// Seed usage: openai served 50, anthropic 5, google none.
	now := time.Now().UTC()
	seed := []struct {
		provider string
		n        int
	}{
		{"openai", 50},
		{"anthropic", 5},
	}
	for _, s := range seed {
		for i := 0; i < s.n; i++ {
			if _, err := repo.RawDB().Exec(
				`INSERT INTO usageHistory (timestamp, provider, model, apiKey, endpoint, promptTokens, completionTokens, cost, status, tokens, meta)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339), s.provider, "m", "k", "/v1/chat/completions", 1, 1, 0, "success", "{}", "{}",
			); err != nil {
				t.Fatalf("seed %s: %v", s.provider, err)
			}
		}
	}

	ladder := []string{
		"openai/gpt-4.1",
		"anthropic/claude-sonnet-4.5",
		"google/gemini-2.5-flash",
		"openai/gpt-4.1-mini",
	}
	got := h.applyLKGPRouting(ctx, ladder)
	if len(got) != 4 {
		t.Fatalf("ladder length changed: %v", got)
	}
	// google (0) first, then anthropic (5), then the two openai entries in
	// their stored relative order.
	if got[0] != "google/gemini-2.5-flash" || got[1] != "anthropic/claude-sonnet-4.5" {
		t.Errorf("head of ladder = %v, want google then anthropic", got)
	}
	if got[2] != "openai/gpt-4.1" || got[3] != "openai/gpt-4.1-mini" {
		t.Errorf("openai tail order = %v, want stored order preserved", got)
	}
}

// A failed read is the neutral answer: the stored order stands.
func TestLKGPFailedReadKeepsOrder(t *testing.T) {
	h := &ChatHandler{Repo: nil}
	ladder := []string{"openai/gpt-4.1", "anthropic/claude-sonnet-4.5"}
	got := h.applyLKGPRouting(t.Context(), ladder)
	if got[0] != "openai/gpt-4.1" || got[1] != "anthropic/claude-sonnet-4.5" {
		t.Errorf("nil repo reordered: %v", got)
	}
}

func setupLKGPRepo(t *testing.T) *db.Repo {
	t.Helper()
	sqlDB, cleanup := setupChatTestDB(t)
	t.Cleanup(cleanup)
	repo := db.NewRepo(sqlDB)
	if _, err := repo.RawDB().Exec(`CREATE TABLE IF NOT EXISTS usageHistory (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		provider TEXT,
		model TEXT,
		connectionId TEXT,
		apiKey TEXT,
		endpoint TEXT,
		promptTokens INTEGER DEFAULT 0,
		completionTokens INTEGER DEFAULT 0,
		cost REAL DEFAULT 0,
		status TEXT,
		tokens TEXT,
		meta TEXT,
		clientKey TEXT DEFAULT '',
		billingTeamId TEXT DEFAULT '',
		comboName TEXT DEFAULT ''
	);`); err != nil {
		t.Fatalf("create usageHistory: %v", err)
	}
	return repo
}
