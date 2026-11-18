// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"9routerx/proxy/internal/budget"
	"9routerx/proxy/internal/db"
	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/log"
	"9routerx/proxy/internal/pricing"
)

// Combo spending budget (N3): a combo can carry its own spend ceiling —
// budgetLimitMicros + budgetPeriod on the combos row — enforced once per
// dispatch, before the member ladder is walked. The measurement is the same
// ledger the per-key budget reads, attributed by the comboName stamp the
// usage insert writes for requests that walked a combo; the same fail-open
// applies, so a storage blip measures zero rather than locking the combo out.
//
// Breach behavior follows the operator's budget.breach_mode, the same knob
// the per-key budget obeys:
//
//	reject     — the generic 402, worded for the combo.
//	downgrade  — the ladder is restricted to the single cheapest member the
//	             pricing table knows, and the response flags the restriction.
//	             A combo whose members are all unpriced has no meaningful
//	             "cheapest", so the refusal stands instead of a guess.

// comboBudgetReader adapts the combo spend read to budget.Reader: the
// engine's Check signature is keyed by "rawKey", which here is the combo
// name.
type comboBudgetReader struct {
	repo *db.Repo
}

func (r comboBudgetReader) SpendForKeySince(ctx context.Context, comboName, since string) (budget.Spend, error) {
	return r.repo.SpendForComboSince(ctx, comboName, since)
}

// comboBudgetGate scores the combo's ceiling before dispatch. A refusal
// returns blocked=true and the response is already written; a downgrade
// returns the restricted member list.
func (h *ChatHandler) comboBudgetGate(ctx context.Context, w http.ResponseWriter, comboName string, comboModels []string) ([]string, bool) {
	if h.Repo == nil || comboName == "" {
		return comboModels, false
	}
	combo, err := h.Repo.GetComboByName(comboName)
	if err != nil || combo == nil || combo.BudgetLimitMicros <= 0 {
		return comboModels, false
	}
	limits := budget.Limits{LimitMicros: combo.BudgetLimitMicros, Period: combo.BudgetPeriod, HardCutoff: true}
	d := budget.Check(ctx, comboBudgetReader{h.Repo}, comboName, limits, time.Now())
	if !d.Refuse {
		return comboModels, false
	}
	if budget.BreachMode() == budget.BreachModeDowngrade {
		if restricted, ok := cheapestComboMembers(comboModels); ok {
			log.Warn("combo-budget", "downgrade",
				"combo", comboName,
				"spent", budget.FormatMicros(d.Spend.Micros),
				"limit", budget.FormatMicros(d.Limits.LimitMicros))
			w.Header().Set("X-Budget-Downgraded", "combo:"+comboName)
			return restricted, false
		}
	}
	rejectComboBudget(w, d, comboName)
	return comboModels, true
}

// cheapestComboMembers narrows the ladder to the single cheapest member the
// pricing table knows, ranked by the same blended per-1M rate the per-key
// downgrade ranks by. Unpriced members are skipped: pricing is what makes
// "cheapest" decidable. Ties break deterministically by entry spelling.
func cheapestComboMembers(members []string) ([]string, bool) {
	best := ""
	var bestRate float64
	found := false
	for _, entry := range members {
		parts := strings.SplitN(entry, "/", 2)
		if len(parts) != 2 {
			continue
		}
		p, known := pricing.GetPricingForModel(parts[0], parts[1])
		if !known {
			continue
		}
		rate := p.InputPer1M + p.OutputPer1M
		if !found || rate < bestRate || (rate == bestRate && entry < best) {
			best, bestRate, found = entry, rate, true
		}
	}
	if !found {
		return nil, false
	}
	return []string{best}, true
}

// rejectComboBudget writes the refusal — the per-key wording's shape, so the
// client contract holds across both budget kinds.
func rejectComboBudget(w http.ResponseWriter, d budget.Decision, comboName string) {
	if !d.ResetAt.IsZero() {
		secs := int(time.Until(d.ResetAt).Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", fmt.Sprint(secs))
	}
	w.Header().Set("X-Budget-Limit", budget.FormatMicros(d.Limits.LimitMicros))
	w.Header().Set("X-Budget-Spent", budget.FormatMicros(d.Spend.Micros))
	handlerutil.WriteJSONError(w, http.StatusPaymentRequired,
		fmt.Sprintf("Budget exhausted for combo %q ($%s of $%s this period).",
			comboName, budget.FormatMicros(d.Spend.Micros), budget.FormatMicros(d.Limits.LimitMicros)))
}
