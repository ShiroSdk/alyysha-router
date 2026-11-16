// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"fmt"
	"testing"
)

func resetRetryCaps(t *testing.T) {
	t.Helper()
	SetRetryAttemptCaps(0, 0, 0) // defaults: exact/provider off, global = maxFallbackAttempts
	t.Cleanup(func() { SetRetryAttemptCaps(0, 0, 0) })
}

// TestFallbackAttemptBudgetExactCutsBeforeGlobal pins (b): with exact=2, a
// body that already spent 2 attempts is cut at 2 — before the global 10.
func TestFallbackAttemptBudgetExactCutsBeforeGlobal(t *testing.T) {
	resetRetryCaps(t)
	SetRetryAttemptCaps(2, 0, 10)
	body := []byte(`{"exact":"cap"}`)

	// Nothing spent yet: the budget carries the exact cap itself.
	budget, exhausted := fallbackAttemptBudget(body, "deepseek")
	if exhausted || budget != 2 {
		t.Fatalf("fresh body budget = %d exhausted=%v, want 2/false", budget, exhausted)
	}

	// Two attempts spent: the budget is zero and the ladder is skipped.
	proxyRecordBodyAttempts(t, body, 2)
	budget, exhausted = fallbackAttemptBudget(body, "deepseek")
	if !exhausted || budget != 0 {
		t.Fatalf("spent body budget = %d exhausted=%v, want 0/true", budget, exhausted)
	}
}

// TestFallbackAttemptBudgetDefaultsBaseline pins (c): empty config keeps the
// baseline — global = maxFallbackAttempts, exact/provider levels off, and
// cross-request spent counts never cut the ladder.
func TestFallbackAttemptBudgetDefaultsBaseline(t *testing.T) {
	resetRetryCaps(t)
	body := []byte(`{"defaults":"rule"}`)
	proxyRecordBodyAttempts(t, body, 25) // even a heavily-spent body

	budget, exhausted := fallbackAttemptBudget(body, "deepseek")
	if exhausted {
		t.Fatal("default config must never report exhausted")
	}
	if budget != maxFallbackAttempts {
		t.Errorf("default budget = %d, want the baseline %d", budget, maxFallbackAttempts)
	}
}

// TestFallbackAttemptBudgetProviderLevel pins the provider level: the same
// body against one provider is cut there, another provider keeps its budget.
func TestFallbackAttemptBudgetProviderLevel(t *testing.T) {
	resetRetryCaps(t)
	SetRetryAttemptCaps(0, 3, 10)
	body := []byte(`{"provider":"level"}`)
	proxyRecordBodyAttempts(t, body, 3)

	if _, exhausted := fallbackAttemptBudget(body, "deepseek"); !exhausted {
		t.Error("provider that spent 3 of cap 3 must be exhausted")
	}
	if _, exhausted := fallbackAttemptBudget(body, "glm"); exhausted {
		t.Error("another provider must keep its own budget")
	}
}

// TestFallbackAttemptRecorderCap pins the recorder: the cap bounds what is
// recorded and Exhausted flips at the cap (used by the fallback loop break).
func TestFallbackAttemptRecorderCap(t *testing.T) {
	rec := NewFallbackAttemptRecorderWithCap(2)
	for i := 0; i < 5; i++ {
		rec.Record(FallbackAttempt{Provider: "p", Model: "m", StatusCode: 503,
			Error: fmt.Sprintf("attempt %d", i)})
	}
	if len(rec.Snapshot()) != 2 {
		t.Errorf("recorded = %d, want the cap 2", len(rec.Snapshot()))
	}
	if !rec.Exhausted() {
		t.Error("recorder must report exhausted at the cap")
	}
	if NewFallbackAttemptRecorderWithCap(0).Exhausted() != true {
		// A zero cap means the ladder was pre-exhausted by the budget.
		t.Error("cap 0 must read exhausted")
	}
}

// proxyRecordBodyAttempts bridges to the proxy store through the public
// helper with the provider left empty for hash-level counts.
func proxyRecordBodyAttempts(t *testing.T, body []byte, n int) {
	t.Helper()
	RecordBodyAttemptsForTest(body, n)
}
