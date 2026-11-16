// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	json "encoding/json/v2"
	"net/http"

	"9routerx/proxy/internal/handlerutil"
)

// HandleApplyPlanToKeys applies one plan to many API keys in a single
// transaction: every key changes or none does. The response reports the
// applied count; a failure names the key that rolled the batch back.
func (h *DashboardHandler) HandleApplyPlanToKeys(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	if planID == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "missing plan id")
		return
	}
	var body struct {
		KeyIDs []string `json:"keyIds"`
	}
	if err := json.UnmarshalRead(r.Body, &body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	defer r.Body.Close()
	applied, err := h.Repo.SetApiKeyPlanBulk(body.KeyIDs, planID)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"applied": applied,
		"planId":  planID,
	})
}
