// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"9routerx/proxy/internal/budget"
)

func budgetTestDecision() budget.Decision {
	return budget.Decision{
		Refuse:  true,
		Axis:    "cost",
		Spend:   budget.Spend{Micros: 5_000_000},
		Limits:  budget.Limits{LimitMicros: 4_000_000},
		ResetAt: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestApplyBudgetDowngradeReroutesToCheapest pins the downgrade: a refused
// budget reroutes the body to the cheapest model that fits, with the flag
// header set.
func TestApplyBudgetDowngradeReroutesToCheapest(t *testing.T) {
	prevMode := budget.BreachMode()
	budget.SetBreachMode(budget.BreachModeDowngrade)
	t.Cleanup(func() { budget.SetBreachMode(prevMode) })

	rec := httptest.NewRecorder()
	ctx := budget.WithBudgetDowngrade(context.Background(), budgetTestDecision())
	body := []byte(`{"model":"deepseek-reasoner","messages":[{"role":"user","content":"hi"}]}`)

	newBody, handled := applyBudgetDowngrade(ctx, rec, body, "deepseek", "deepseek-reasoner")
	if !handled {
		t.Fatal("marked refusal not handled")
	}
	if got := rec.Header().Get("X-Budget-Downgraded"); !strings.HasPrefix(got, "deepseek-reasoner->") {
		t.Errorf("X-Budget-Downgraded = %q, want deepseek-reasoner-><cheaper>", got)
	}
	newStr := string(newBody)
	if !strings.Contains(newStr, `"model":"`) || strings.Contains(newStr, `"model":"deepseek-reasoner"`) {
		t.Errorf("rerouted body still names the expensive model: %s", newStr)
	}
}

// TestApplyBudgetDowngradeNoCandidateWritesGeneric402 pins the refusal
// contract: when no candidate fits, the generic 402 wording the middleware
// writes is the answer.
func TestApplyBudgetDowngradeNoCandidateWritesGeneric402(t *testing.T) {
	prevMode := budget.BreachMode()
	budget.SetBreachMode(budget.BreachModeDowngrade)
	t.Cleanup(func() { budget.SetBreachMode(prevMode) })

	rec := httptest.NewRecorder()
	ctx := budget.WithBudgetDowngrade(context.Background(), budgetTestDecision())
	body := []byte(`{"model":"deepseek-reasoner","messages":[{"role":"user","content":"hi"}]}`)

	// A provider with no registry models offers no candidate.
	newBody, handled := applyBudgetDowngrade(ctx, rec, body, "provider-without-models", "whatever")
	if !handled {
		t.Fatal("marked refusal not handled")
	}
	if rec.Code != 402 {
		t.Errorf("status = %d, want 402", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Budget exhausted for this API key") {
		t.Errorf("body = %s, want the generic wording", rec.Body.String())
	}
	_ = newBody
}

// TestBreachModeContract pins the mode holder: reject is the default,
// downgrade round-trips, and an unknown value falls back to reject — the
// middleware only marks a refusal in downgrade mode, so the chat-side
// downgrade path is unreachable under reject.
func TestBreachModeContract(t *testing.T) {
	t.Cleanup(func() { budget.SetBreachMode(budget.BreachModeReject) })
	if budget.BreachMode() != budget.BreachModeReject {
		t.Errorf("default breach mode = %q, want reject", budget.BreachMode())
	}
	budget.SetBreachMode(budget.BreachModeDowngrade)
	if budget.BreachMode() != budget.BreachModeDowngrade {
		t.Errorf("BreachMode = %q after set, want downgrade", budget.BreachMode())
	}
	budget.SetBreachMode("garbage")
	if budget.BreachMode() != budget.BreachModeReject {
		t.Errorf("unknown mode = %q, want reject fallback", budget.BreachMode())
	}
}
