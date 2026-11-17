// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"net/http"
	"strconv"

	"9routerx/proxy/internal/handlerutil"
)

// HandleGetSettingsHistory serves GET /api/settings/history?key=K: the newest
// history entries for one key (or the whole blob when the key is empty).
// Credential keys are masked in the stored rows, so the response never leaks
// a secret.
func (h *DashboardHandler) HandleGetSettingsHistory(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	entries, err := h.Repo.GetSettingsHistory(r.URL.Query().Get("key"), limit)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
	})
}

// HandleRollbackSettingsKey serves POST /api/settings/history/{id}/rollback:
// restores the key's old value from the named history row (credential keys
// are refused — rotate them in the panel instead) and the rollback itself is
// recorded as history.
func (h *DashboardHandler) HandleRollbackSettingsKey(w http.ResponseWriter, r *http.Request) {
	entryID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || entryID <= 0 {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid history id")
		return
	}
	source := "dashboard_rollback"
	if err := h.Repo.RollbackSettingsKey(entryID, source); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"rolledBack": entryID})
}
