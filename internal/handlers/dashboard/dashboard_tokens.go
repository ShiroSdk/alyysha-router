// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	json "encoding/json/v2"

	"9routerx/proxy/internal/db"
	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/keikey"
	"9routerx/proxy/internal/middleware"
)

// Dashboard access tokens (N5 RBAC): the operator mints a scoped bearer
// credential (admin | write | read) for scripting callers instead of sharing
// the dashboard session. The plaintext is shown exactly once, in the mint
// response — the store keeps only the Argon2id verifier, the same primitives
// the engine keys use.

// mintDashboardTokenBytes is the entropy of a minted token: 32 bytes of CSPRNG
// output, hex-encoded — unguessable, URL-safe, and far past anything a brute
// force against the Argon2id verifier can reach.
const mintDashboardTokenBytes = 32

// authorizeDashboardTokenAdmin admits the mint/list/revoke surfaces: a
// dashboard session, the CLI token, or an admin-scope access token. The
// open-install caller (no key, login off) is admitted too — it is the same
// trust every other management write extends to that mode.
func (h *DashboardHandler) authorizeDashboardTokenAdmin(w http.ResponseWriter, r *http.Request) bool {
	if middleware.CallerIsDashboardPrivileged(h.Repo, r) {
		return true
	}
	if scope, ok := middleware.AccessTokenScope(h.Repo, r); ok && scope == db.DashboardTokenScopeAdmin {
		return true
	}
	handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Unauthorized: admin session, CLI token or admin-scope token required")
	return false
}

// HandleListDashboardTokens handles GET /api/dashboard-tokens.
func (h *DashboardHandler) HandleListDashboardTokens(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeDashboardTokenAdmin(w, r) {
		return
	}
	tokens, err := h.Repo.ListDashboardTokens(r.Context())
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// HandleMintDashboardToken handles POST /api/dashboard-tokens {name, scope}.
// The response carries the plaintext once; it is never retrievable again.
func (h *DashboardHandler) HandleMintDashboardToken(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeDashboardTokenAdmin(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()
	var req struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "missing token name")
		return
	}
	if !db.IsValidDashboardTokenScope(req.Scope) {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "scope must be admin, write or read")
		return
	}
	raw := make([]byte, mintDashboardTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "entropy failure")
		return
	}
	plaintext := "9rdt_" + hex.EncodeToString(raw)
	tokenHash, err := keikey.Hash(plaintext)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := newDashboardTokenID()
	if err := h.Repo.CreateDashboardToken(r.Context(), id, req.Name, tokenHash, keikey.LookupHash(plaintext), req.Scope); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A mint changes what the gates admit; any cached resolution of a
	// revoked-and-reminted spelling must not outlive the write.
	middleware.InvalidateDashboardTokenCache()
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":           id,
		"name":         req.Name,
		"scope":        req.Scope,
		"token":        plaintext,
		"header":       middleware.DashboardAccessTokenHeader,
		"expiresHint":  "until revoked",
		"createdAt":    time.Now().UTC().Format(time.RFC3339),
	})
}

// HandleRevokeDashboardToken handles DELETE /api/dashboard-tokens/{id}.
func (h *DashboardHandler) HandleRevokeDashboardToken(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeDashboardTokenAdmin(w, r) {
		return
	}
	id := getURLParam(r, "id")
	if id == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "missing token id")
		return
	}
	if err := h.Repo.RevokeDashboardToken(r.Context(), id); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	middleware.InvalidateDashboardTokenCache()
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"status": "revoked", "id": id})
}

// newDashboardTokenID is a random row id; it names nothing sensitive.
func newDashboardTokenID() string {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	return "dtok_" + hex.EncodeToString(raw)
}
