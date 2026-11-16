// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	json "encoding/json/v2"

	"9routerx/proxy/internal/budget"
	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/pricing"
	"9routerx/proxy/internal/providers"
)

// Budget breach downgrade: when the operator's breach mode is "downgrade",
// a refused budget rides the request (the middleware marks it) and this
// side reroutes the turn to the cheapest model on the same provider that
// still fits the context window. The refusal contract does not change —
// when nothing fits, the same generic 402 the middleware writes is the
// answer, and "reject" mode never reaches this code.

// cheapestModelFor picks the cheapest model on the provider that fits the
// estimated prompt, ranked by the blended per-1M rate the pricing table
// carries. Models without pricing rank last, deterministically by name.
func cheapestModelFor(provider string, models []string, estimatedTokens int) (model string, rate float64, ok bool) {
	type candidate struct {
		id    string
		rate  float64
		known bool
	}
	candidates := make([]candidate, 0, len(models))
	for _, m := range models {
		if m == "" || strings.EqualFold(m, provider) {
			continue
		}
		// Context guard reuse: the candidate's window must hold the prompt.
		if window, fits := providerContextFits(m, estimatedTokens); !fits && window > 0 {
			continue
		}
		c := candidate{id: m}
		if p, known := pricing.GetPricingForModel(provider, m); known {
			c.rate = p.InputPer1M + p.OutputPer1M
			c.known = true
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return "", 0, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].known != candidates[j].known {
			return candidates[i].known
		}
		if candidates[i].rate != candidates[j].rate {
			return candidates[i].rate < candidates[j].rate
		}
		return candidates[i].id < candidates[j].id
	})
	return candidates[0].id, candidates[0].rate, true
}

// applyBudgetDowngrade handles a marked refusal. The reroute rewrites the
// model in the body and flags the response header; the same generic 402 the
// middleware writes is the answer when no candidate fits.
func applyBudgetDowngrade(ctx context.Context, w http.ResponseWriter, body []byte, provider, model string) ([]byte, bool) {
	d, marked := budget.BudgetDowngradeFrom(ctx)
	if !marked || !d.Refuse {
		return body, false
	}
	est := estimatePromptTokens(body)

	models := []string{}
	if row := providers.ProviderModels[provider]; row != nil {
		models = append(models, row...)
	}
	cheapest, _, ok := cheapestModelFor(provider, models, est)
	if !ok || strings.EqualFold(cheapest, model) {
		// Nothing cheaper fits: the generic refusal stands, worded exactly
		// like the middleware's reject path so the client contract holds.
		rejectBudgetGeneric(w, d)
		return body, true
	}
	w.Header().Set("X-Budget-Downgraded", model+"->"+cheapest)
	newBody, err := rewriteBodyModel(body, cheapest)
	if err != nil {
		rejectBudgetGeneric(w, d)
		return body, true
	}
	return newBody, true
}

// rewriteBodyModel swaps the model field of an OpenAI-shaped body.
func rewriteBodyModel(body []byte, newModel string) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	raw["model"] = newModel
	return json.Marshal(raw, json.Deterministic(true))
}

// rejectBudgetGeneric writes the middleware's exact refusal wording — the
// downgrade path must not change the error contract.
func rejectBudgetGeneric(w http.ResponseWriter, d budget.Decision) {
	if !d.ResetAt.IsZero() {
		secs := int(time.Until(d.ResetAt).Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", fmt.Sprint(secs))
	}
	if d.Axis == "cost" {
		w.Header().Set("X-Budget-Limit", budget.FormatMicros(d.Limits.LimitMicros))
		w.Header().Set("X-Budget-Spent", budget.FormatMicros(d.Spend.Micros))
		handlerutil.WriteJSONError(w, http.StatusPaymentRequired,
			fmt.Sprintf("Budget exhausted for this API key ($%s of $%s this %s period).",
				budget.FormatMicros(d.Spend.Micros), budget.FormatMicros(d.Limits.LimitMicros), d.Axis))
		return
	}
	handlerutil.WriteJSONError(w, http.StatusPaymentRequired,
		fmt.Sprintf("Budget exhausted for this API key (%s of %s tokens this %s period).",
			fmt.Sprint(d.Spend.Tokens), fmt.Sprint(d.Limits.LimitTokens), d.Axis))
}
