// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	json "encoding/json/v2"
	"net/http"
	"os"
	"strings"

	"9routerx/proxy/internal/clientconfig"
	"9routerx/proxy/internal/handlerutil"
)

// HandleConnectClient runs the clientconfig adapter logic server-side: the
// dashboard's Connect buttons drive the same patcher the CLI command uses,
// against the config files on this machine. The credential comes from the
// dashboard session (an existing gateway key), never from a query string.
func (h *DashboardHandler) HandleConnectClient(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Client  string `json:"client"`
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
		Model   string `json:"model"`
		DryRun  bool   `json:"dryRun"`
		// Home overrides the directory the adapter resolves the client config
		// under — the same seam the CLI's --home flag carries. Production
		// always sends it empty and gets the server's real home dir.
		Home string `json:"home,omitempty"`
	}
	if err := json.UnmarshalRead(r.Body, &body); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	adapter, err := clientconfig.Resolve(body.Client)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest,
			"unknown client: "+body.Client+" (the nine adapters are listed in the picker)")
		return
	}
	home := body.Home
	if home == "" {
		if v, err := os.UserHomeDir(); err == nil {
			home = v
		}
	}
	opts := clientconfig.Options{
		Home:    home,
		BaseURL: strings.TrimSpace(body.BaseURL),
		APIKey:  strings.TrimSpace(body.APIKey),
		Model:   strings.TrimSpace(body.Model),
	}
	plans, err := adapter.Plan(opts)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := clientconfig.AssertNoForbiddenEdits(plans); err != nil {
		handlerutil.WriteJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	edits := make([]map[string]any, 0)
	for _, plan := range plans {
		for _, edit := range plan.Edits {
			value := edit.Value
			lower := strings.ToLower(strings.Join(edit.KeyPath, "."))
			if strings.Contains(lower, "token") || strings.Contains(lower, "auth") || strings.Contains(lower, "api") {
				if len(value) > 8 {
					value = value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
				} else {
					value = strings.Repeat("*", len(value))
				}
			}
			edits = append(edits, map[string]any{
				"file":    plan.File.Path,
				"keyPath": strings.Join(edit.KeyPath, "."),
				"value":   value,
				"delete":  edit.Delete,
			})
		}
	}
	if body.DryRun {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"dryRun": true,
			"edits":  edits,
		})
		return
	}
	result, err := adapter.Apply(opts)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"changed": result.Changed,
		"backups": result.Backups,
	})
}
