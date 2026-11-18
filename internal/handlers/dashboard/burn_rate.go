// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"net/http"
	"time"

	"9routerx/proxy/internal/handlerutil"
)

// HandleGetBurnRate serves GET /api/burn-rate: the 24-hour sliding token and
// cost burn rate with a projection of how many hours remain before each
// active budget is exhausted. Read-only, computed from existing usageHistory
// rows — no new table, no scheduler.
func (h *DashboardHandler) HandleGetBurnRate(w http.ResponseWriter, r *http.Request) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	var tokens int64
	var cost float64
	err := h.Repo.RawDB().QueryRow(
		`SELECT COALESCE(SUM(promptTokens + completionTokens), 0), COALESCE(SUM(cost), 0)
		 FROM usageHistory WHERE timestamp >= ?`,
		cutoff.Format("2006-01-02T15:04:05Z"),
	).Scan(&tokens, &cost)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Budget projections for keys with an armed budget.
	var budgets []map[string]any
	rows, err := h.Repo.RawDB().Query(
		`SELECT id, COALESCE(dailyCostLimit, 0) FROM apiKeys WHERE dailyCostLimit > 0`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var limit float64
			if err := rows.Scan(&id, &limit); err != nil {
				continue
			}
			if cost > 0 && limit > 0 {
				hoursLeft := (limit - cost) / (cost / 24)
				if hoursLeft < 0 {
					hoursLeft = 0
				}
				budgets = append(budgets, map[string]any{
					"keyId": id, "spentUsd": cost, "limitUsd": limit,
					"hoursLeft": hoursLeft,
				})
			}
		}
	}
	if budgets == nil {
		budgets = []map[string]any{}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"windowHours":    24,
		"tokens":         tokens,
		"costUsd":        cost,
		"tokensPerHour":  float64(tokens) / 24,
		"costPerHourUsd": cost / 24,
		"budgets":        budgets,
	})
}
