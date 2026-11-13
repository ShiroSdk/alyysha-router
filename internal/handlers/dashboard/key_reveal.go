// Alyysha Router — built by YannClipper — https://t.me/YannClippers
package dashboard

import (
	"net/http"
	"strings"

	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/log"
	"alyysha/proxy/internal/middleware"
)

func protectKeyReveal(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func logKeyReveal(r *http.Request, kind, id string) {
	masked := strings.Repeat("*", handlerutil.RuneCount(id))
	if handlerutil.RuneCount(id) > 8 {
		masked = handlerutil.RunePrefix(id, 4) + "…" + handlerutil.RuneSuffix(id, 4)
	}
	log.InfoCtx(r.Context(), "apikeys", "apikeys reveal", "resource_kind", kind, "key_id", masked, "remote_addr", r.RemoteAddr)
}

// HandleRevealApiKey handles GET /api/keys/{id}/reveal.
//
// A key created with revealable=true keeps a Vault-sealed copy of its
// plaintext; this opens it and returns the value to a dashboard-privileged
// caller. A hash-only key has nothing to open and answers 404 rather than a
// partial value — the row never recorded a recoverable secret, so the only
// path forward is rotate.
//
// The response carries no-store headers (protectKeyReveal) so the plaintext
// does not survive in an intermediary or browser cache.
func (h *DashboardHandler) HandleRevealApiKey(w http.ResponseWriter, r *http.Request) {
	id := getURLParam(r, "id")
	if id == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "missing apiKey id")
		return
	}
	if !middleware.CallerIsDashboardPrivileged(h.Repo, r) {
		handlerutil.WriteJSONError(w, http.StatusForbidden, "dashboard credentials required")
		return
	}
	plaintext, err := h.Repo.RevealApiKey(id)
	if err != nil {
		// A hash-only key (or an unknown id) has no recoverable secret. It is
		// not a server error — the only path forward is rotate, so 404 it.
		if strings.Contains(err.Error(), "no such api key") {
			handlerutil.WriteJSONError(w, http.StatusNotFound, "key is not revealable — rotate it instead")
			return
		}
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logKeyReveal(r, "client_key_reveal", id)
	protectKeyReveal(w)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":  id,
		"key": plaintext,
	})
}
