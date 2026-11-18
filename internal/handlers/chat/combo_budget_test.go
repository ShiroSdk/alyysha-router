// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"net/http/httptest"
	"testing"

	"9routerx/proxy/internal/budget"
	"9routerx/proxy/internal/db"
)

// comboBudgetTestSetup builds a repo whose combos table carries one budgeted
// combo ("budgeted": limit + period) and one unbounded combo ("open"), and
// restores the global breach mode afterwards.
func comboBudgetTestSetup(t *testing.T, limitMicros int64, period string) *db.Repo {
	t.Helper()
	sqlDB, cleanup := setupChatTestDB(t)
	repo := db.NewRepo(sqlDB)
	t.Cleanup(cleanup)
	if err := repo.CreateCombo("cb-n3-1", "n3-budgeted", "", `["openai/gpt-4o-mini","openai/gpt-4.1"]`, "fallback"); err != nil {
		t.Fatalf("create budgeted combo: %v", err)
	}
	if err := repo.CreateCombo("cb-n3-2", "n3-open", "", `["openai/gpt-4o-mini"]`, "fallback"); err != nil {
		t.Fatalf("create open combo: %v", err)
	}
	if limitMicros > 0 {
		if err := repo.SetComboBudget("cb-n3-1", limitMicros, period); err != nil {
			t.Fatalf("set budget: %v", err)
		}
	}
	t.Cleanup(func() { budget.SetBreachMode(budget.BreachModeReject) })
	return repo
}

// The spend the gate measures comes from the combo-stamped rows the usage
// insert writes, so the test stamps the ledger directly the same way.
func stampComboSpend(t *testing.T, repo *db.Repo, combo string, cost float64) {
	t.Helper()
	if _, err := repo.RawDB().Exec(
		`INSERT INTO usageHistory (timestamp, provider, model, apiKey, endpoint, promptTokens, completionTokens, cost, status, tokens, meta, comboName)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"2026-01-01T00:00:00Z", "openai", "gpt-4o-mini", "k", "/v1/chat/completions", 1, 1, cost, "success", "{}", "{}", combo,
	); err != nil {
		t.Fatalf("stamp spend: %v", err)
	}
}

func TestComboBudgetGateUnboundedPasses(t *testing.T) {
	repo := comboBudgetTestSetup(t, 0, "")
	h := &ChatHandler{Repo: repo}
	rec := httptest.NewRecorder()
	members := []string{"openai/gpt-4o-mini", "openai/gpt-4.1"}
	got, blocked := h.comboBudgetGate(context.Background(), rec, "n3-open", members)
	if blocked {
		t.Fatal("unbounded combo was blocked")
	}
	if len(got) != len(members) {
		t.Errorf("members changed: %v", got)
	}
}

func TestComboBudgetGateRejectMode(t *testing.T) {
	repo := comboBudgetTestSetup(t, 1_000_000, budget.PeriodTotal)
	stampComboSpend(t, repo, "n3-budgeted", 2.0) // past the $1 ceiling
	h := &ChatHandler{Repo: repo}
	rec := httptest.NewRecorder()
	_, blocked := h.comboBudgetGate(context.Background(), rec, "n3-budgeted", []string{"openai/gpt-4o-mini"})
	if !blocked {
		t.Fatal("breached combo was not blocked")
	}
	if rec.Code != 402 {
		t.Errorf("status %d, want 402", rec.Code)
	}
	if v := rec.Header().Get("X-Budget-Limit"); v != "1.00" {
		t.Errorf("X-Budget-Limit = %q, want 1.00", v)
	}
}

func TestComboBudgetGateDowngradeMode(t *testing.T) {
	repo := comboBudgetTestSetup(t, 1_000_000, budget.PeriodTotal)
	stampComboSpend(t, repo, "n3-budgeted", 2.0)
	budget.SetBreachMode(budget.BreachModeDowngrade)
	h := &ChatHandler{Repo: repo}
	rec := httptest.NewRecorder()
	members := []string{"openai/gpt-4o-mini", "openai/gpt-4.1"}
	got, blocked := h.comboBudgetGate(context.Background(), rec, "n3-budgeted", members)
	if blocked {
		t.Fatal("downgrade mode blocked instead of restricting")
	}
	if len(got) != 1 {
		t.Fatalf("restricted members = %v, want the single cheapest", got)
	}
	// The pricing table decides which member is cheapest; the header flags
	// the restriction so the response carries the audit trail.
	if v := rec.Header().Get("X-Budget-Downgraded"); v != "combo:n3-budgeted" {
		t.Errorf("X-Budget-Downgraded = %q", v)
	}
	// The survivor must be a member of the original ladder.
	found := false
	for _, m := range members {
		if got[0] == m {
			found = true
		}
	}
	if !found {
		t.Errorf("restricted to a non-member: %q", got[0])
	}
}

func TestComboBudgetGateBelowCeilingPasses(t *testing.T) {
	repo := comboBudgetTestSetup(t, 1_000_000, budget.PeriodTotal)
	stampComboSpend(t, repo, "n3-budgeted", 0.5) // under the ceiling
	h := &ChatHandler{Repo: repo}
	rec := httptest.NewRecorder()
	_, blocked := h.comboBudgetGate(context.Background(), rec, "n3-budgeted", []string{"openai/gpt-4o-mini"})
	if blocked {
		t.Fatal("combo under its ceiling was blocked")
	}
}

func TestCheapestComboMembersUnpricedRefuses(t *testing.T) {
	// A member list the pricing table knows nothing about has no meaningful
	// "cheapest" — the gate answers the refusal rather than guessing.
	if got, ok := cheapestComboMembers([]string{"nosuch/gpt-x", "nosuch/y"}); ok {
		t.Errorf("unpriced members narrowed to %v", got)
	}
}

func TestComboBudgetGateNilRepo(t *testing.T) {
	h := &ChatHandler{}
	rec := httptest.NewRecorder()
	members := []string{"openai/gpt-4o-mini"}
	got, blocked := h.comboBudgetGate(context.Background(), rec, "anything", members)
	if blocked || len(got) != len(members) {
		t.Errorf("nil repo intervened: blocked=%v got=%v", blocked, got)
	}
}
