// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"net/http"
	"strings"

	"9routerx/proxy/internal/auth"
	"9routerx/proxy/internal/handlerutil"
)

// Onboarding (N13): the first-run state the tour reads and the dismissal the
// operator writes. The status is derived, never stored — a fresh install is
// "not onboarded" by definition, and the dismissal is one settings flag the
// operator sets by pressing "Done", so a restore restores the answer too.

// HandleGetOnboardingStatus handles GET /api/onboarding/status.
func (h *DashboardHandler) HandleGetOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	conns, err := h.Repo.GetProviderConnections("", false)
	activeConns := 0
	if err == nil {
		for _, c := range conns {
			if c.IsActive == 1 {
				activeConns++
			}
		}
	}
	keys, err := h.Repo.GetApiKeys()
	activeKeys := 0
	if err == nil {
		for _, k := range keys {
			if k.IsActive == 1 {
				activeKeys++
			}
		}
	}

	dismissed := false
	if raw, err := h.Repo.GetSettingsRaw(); err == nil && raw != nil {
		if v, ok := raw["onboardingCompleted"].(bool); ok {
			dismissed = v
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"hasConnection": activeConns > 0,
		"activeConns":   activeConns,
		"hasKey":        activeKeys > 0,
		"activeKeys":    activeKeys,
		"loginRequired": auth.RequireLogin(h.Repo),
		"dismissed":     dismissed,
		"complete":      activeConns > 0 && activeKeys > 0,
	})
}

// HandlePostOnboardingComplete handles POST /api/onboarding/complete: the
// operator's dismissal rides the settings blob like every other flag, so the
// section normalizer contract applies — the write carries the whole key, and
// nothing else in the blob is touched.
func (h *DashboardHandler) HandlePostOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	raw, err := h.Repo.GetSettingsRaw()
	if err != nil {
		raw = map[string]any{}
	}
	if raw == nil {
		raw = map[string]any{}
	}
	raw["onboardingCompleted"] = true
	if err := h.Repo.UpdateSettingsRaw(raw); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// unusedStringsKeeper keeps strings imported for the sanitize map literals
// this file's neighbors use; removed if the import drops again.
var _ = strings.TrimSpace
