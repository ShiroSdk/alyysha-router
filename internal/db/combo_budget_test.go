// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package db

import (
	"context"
	"testing"
	"time"

	"alyysha/proxy/internal/budget"
)

// The N3 combo budget: the ceiling persists on the combo row, the spend
// read attributes the ledger rows stamped with the combo name.
func TestComboBudgetRoundTrip(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	if err := repo.CreateCombo("cb-1", "budgeted", "", `["openai/gpt-4o-mini"]`, "fallback"); err != nil {
		t.Fatalf("create combo: %v", err)
	}
	if err := repo.SetComboBudget("cb-1", 5_000_000, budget.PeriodDaily); err != nil {
		t.Fatalf("set budget: %v", err)
	}
	combo, err := repo.GetComboByName("budgeted")
	if err != nil || combo == nil {
		t.Fatalf("get combo: %v (nil=%v)", err, combo == nil)
	}
	if combo.BudgetLimitMicros != 5_000_000 {
		t.Errorf("budgetLimitMicros = %d, want 5000000", combo.BudgetLimitMicros)
	}
	if combo.BudgetPeriod != budget.PeriodDaily {
		t.Errorf("budgetPeriod = %q, want daily", combo.BudgetPeriod)
	}

	// Clearing: a non-positive limit disarms the ceiling and clears the
	// period, so the stored row reads as unbounded again.
	if err := repo.SetComboBudget("cb-1", 0, ""); err != nil {
		t.Fatalf("clear budget: %v", err)
	}
	combo, _ = repo.GetComboByName("budgeted")
	if combo == nil || combo.BudgetLimitMicros != 0 || combo.BudgetPeriod != "" {
		t.Errorf("cleared combo = %+v", combo)
	}

	// An unknown period is stored as sent: the enforcement read resolves it
	// (unknown reads as monthly), so storing the operator's spelling keeps
	// the row honest without a second validation to drift.
	if err := repo.SetComboBudget("cb-1", 1_000_000, "fortnightly"); err != nil {
		t.Fatalf("set odd period: %v", err)
	}
	combo, _ = repo.GetComboByName("budgeted")
	if combo == nil || combo.BudgetPeriod != "fortnightly" {
		t.Errorf("odd period stored = %+v", combo)
	}
}

func TestSpendForComboSince(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	if err := repo.CreateCombo("cb-2", "tracked", "", `["openai/gpt-4o-mini"]`, "fallback"); err != nil {
		t.Fatalf("create combo: %v", err)
	}

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)
	fresh := now.Add(-1 * time.Hour).Format(time.RFC3339)
	seed := []struct {
		ts     string
		combo  string
		cost   float64
		tokens int
	}{
		{old, "tracked", 1.5, 100},
		{fresh, "tracked", 2.25, 200},
		{fresh, "", 9.99, 400},
		{fresh, "other", 0.5, 50},
	}
	for i, r := range seed {
		if _, err := repo.RawDB().Exec(
			`INSERT INTO usageHistory (timestamp, provider, model, apiKey, endpoint, promptTokens, completionTokens, cost, status, tokens, meta, comboName)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ts, "openai", "gpt-4o-mini", "key", "/v1/chat/completions", r.tokens/2, r.tokens/2, r.cost, "success", "{}", "{}", r.combo,
		); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	// Whole ledger: only the stamped rows count.
	spend, err := repo.SpendForComboSince(context.Background(), "tracked", "")
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if spend.Micros != 1_500_000+2_250_000 {
		t.Errorf("micros = %d, want 3750000", spend.Micros)
	}
	if spend.Tokens != 300 {
		t.Errorf("tokens = %d, want 300", spend.Tokens)
	}

	// Windowed: the old row drops out.
	spend, err = repo.SpendForComboSince(context.Background(), "tracked",
		now.Add(-24*time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("windowed spend: %v", err)
	}
	if spend.Micros != 2_250_000 {
		t.Errorf("windowed micros = %d, want 2250000", spend.Micros)
	}

	// A combo with no rows is zero spend, not an error.
	spend, err = repo.SpendForComboSince(context.Background(), "untracked", "")
	if err != nil {
		t.Fatalf("empty combo spend: %v", err)
	}
	if spend.Micros != 0 || spend.Tokens != 0 {
		t.Errorf("empty combo = %+v", spend)
	}

	// An empty name reads nothing rather than every row.
	spend, err = repo.SpendForComboSince(context.Background(), "", "")
	if err != nil {
		t.Fatalf("empty name: %v", err)
	}
	if spend.Micros != 0 {
		t.Errorf("empty name micros = %d, want 0", spend.Micros)
	}
}

// The combo attribution the insert writes is what the spend read sums; the
// stamp rides the variadic position the doc comment names.
func TestInsertUsageHistoryComboStamp(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	if err := repo.InsertUsageHistory("openai", "gpt-4o-mini", "conn", "raw", "/v1/chat/completions",
		10, 5, 0.25, "success", 15, "{}", "[]", "client", "team", "tracked"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := repo.InsertUsageHistory("openai", "gpt-4o-mini", "conn", "raw", "/v1/chat/completions",
		10, 5, 0.25, "success", 15, "{}", "[]", "client", "team"); err != nil {
		t.Fatalf("insert without combo: %v", err)
	}
	spend, err := repo.SpendForComboSince(context.Background(), "tracked", "")
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if spend.Micros != 250_000 || spend.Tokens != 15 {
		t.Errorf("stamped spend = %+v, want 250000 micros / 15 tokens", spend)
	}
}
