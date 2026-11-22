// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package dashboard

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"alyysha/proxy/internal/browsercookies"
	"alyysha/proxy/internal/browserlogin"
	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/log"

	"github.com/go-chi/chi/v5"
)

// Generic web-login handlers (weblogin_generic.go): the throwaway-browser
// sign-in and the signed-in-browser import for every provider whose
// credential is described by a WebLoginConfig. Providers without a config
// answer 404 by name, and the dedicated gemini/deepseek flows keep their
// concrete routes — chi resolves static segments before parameters, so the
// generic {id} route only catches the providers nothing concrete serves.

// providerWebLoginTimeout bounds one throwaway-browser sign-in: the
// operator has to complete the provider's sign-in inside the window.
const providerWebLoginTimeout = 4 * time.Minute

// providerImportChromeTimeout bounds one browser-profile read.
const providerImportChromeTimeout = 60 * time.Second

// providerWebBrowserLoginRun is the runner seam the tests stand in for.
var providerWebBrowserLoginRun = browserlogin.RunProvider

// HandleProviderWebBrowserLogin handles POST /api/providers/{id}/browser-login
// for every provider with a WebLoginConfig.
func (h *DashboardHandler) HandleProviderWebBrowserLogin(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	cfg, ok := browserlogin.ConfigFor(providerID)
	if !ok {
		handlerutil.WriteJSONError(w, http.StatusNotFound,
			"no web-login flow is configured for provider "+providerID)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), providerWebLoginTimeout)
	defer cancel()
	log.Info(providerID, "web-login requested", "login_url", cfg.LoginURL)
	result, err := providerWebBrowserLoginRun(ctx, providerID)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, browserlogin.ErrNoBrowser):
			status = http.StatusNotImplemented
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		}
		log.Warn(providerID, "the web-login did not finish", "error", err)
		handlerutil.WriteJSONError(w, status, err.Error())
		return
	}

	log.Info(providerID, "web-login captured the session")
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"provider":  providerID,
		"cookie":    result.CookieHeader,
		"tokens":    result.Tokens,
		"userAgent": result.UserAgent,
	})
}

// HandleProviderWebImportChrome handles POST /api/providers/{id}/import-chrome:
// the credential read out of a browser the operator is already signed in to.
// The credential is read, never written — the caller decides which
// connection it belongs to.
func (h *DashboardHandler) HandleProviderWebImportChrome(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	cfg, ok := browserlogin.ConfigFor(providerID)
	if !ok {
		handlerutil.WriteJSONError(w, http.StatusNotFound,
			"no web-login flow is configured for provider "+providerID)
		return
	}
	request, okReq := decodeImportChromeRequest(w, r)
	if !okReq {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), providerImportChromeTimeout)
	defer cancel()

	dbPath, err := browserCookiesFindDB(strings.TrimSpace(request.Profile))
	if err != nil {
		log.Warn(providerID, "no browser cookie database was found", "error", err)
		handlerutil.WriteJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	cookies, err := browserCookiesReadDomain(ctx, dbPath, cfg.CookieDomain)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, browsercookies.ErrNoKey) {
			status = http.StatusNotImplemented
		}
		log.Warn(providerID, "the browser's cookies could not be read", "path", dbPath, "error", err)
		handlerutil.WriteJSONError(w, status, err.Error())
		return
	}

	// localStorage sources need the profile, the origin and the key; a
	// profile that misses one is a profile that is not signed in.
	tokens := map[string]string{}
	for _, src := range cfg.TokenSources {
		if src.Kind != browserlogin.TokenSourceLocalStorage {
			continue
		}
		profile := browsercookies.ProfileFromCookieDB(dbPath)
		value, err := browsercookies.ReadLocalStorage(profile, originOf(cfg.LoginURL), src.Name)
		if err != nil {
			log.Warn(providerID, "the profile holds no localStorage token", "key", src.Name, "error", err)
			handlerutil.WriteJSONError(w, http.StatusNotFound,
				"that browser profile holds no "+providerID+" credential for "+src.Name+" — sign in to "+cfg.LoginURL+" in it first")
			return
		}
		tokens[src.Name] = value
	}

	log.Info(providerID, "a credential was read out of a browser", "path", dbPath)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"provider": providerID,
		"cookie":   cookies.Header,
		"tokens":   tokens,
		"warning":  importChromeWarning(dbPath, cookies),
	})
}

// originOf derives scheme://host from a login URL.
func originOf(loginURL string) string {
	trimmed := strings.TrimSpace(loginURL)
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		rest := trimmed[idx+3:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			return trimmed[:idx+3] + rest[:slash]
		}
		return trimmed
	}
	return trimmed
}

// HandleProviderResetProfile handles POST /api/providers/{id}/reset-profile:
// the operator deleting a persistent browser profile. The directory carries
// credentials, so the operator must be able to wipe it.
func (h *DashboardHandler) HandleProviderResetProfile(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	removed, err := browserlogin.ResetProfile(providerID)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Info(providerID, "the persistent browser profile was reset", "path", removed)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"provider": providerID,
		"status":   "reset",
	})
}
