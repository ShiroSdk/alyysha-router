// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"context"

	"github.com/go-chi/chi/v5"

	"alyysha/proxy/internal/browsercookies"
	"alyysha/proxy/internal/browserlogin"
)

// TestProviderWebLoginUnknownProvider answers 404 by name: a provider
// without a WebLoginConfig never falls into a flow that cannot capture its
// credential.
func TestProviderWebLoginUnknownProvider(t *testing.T) {
	h := NewDashboardHandler(nil)
	router := chi.NewRouter()
	router.Post("/api/providers/{id}/browser-login", h.HandleProviderWebBrowserLogin)
	router.Post("/api/providers/{id}/import-chrome", h.HandleProviderWebImportChrome)

	req := httptest.NewRequest(http.MethodPost, "/api/providers/some-random/browser-login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("browser-login status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "some-random") {
		t.Errorf("body does not name the provider: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/providers/some-random/import-chrome", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("import-chrome status = %d, want 404", rec.Code)
	}
}

// The generic browser-login runs the provider's flow and answers cookie +
// tokens; a failed run maps onto the same statuses the dedicated flows use.
func TestProviderWebLoginRunsConfiguredFlow(t *testing.T) {
	h := NewDashboardHandler(nil)
	router := chi.NewRouter()
	router.Post("/api/providers/{id}/browser-login", h.HandleProviderWebBrowserLogin)

	// Stand in for the runner: capture the provider id it was called with.
	var calledWith string
	prev := providerWebBrowserLoginRun
	providerWebBrowserLoginRun = func(_ context.Context, provider string) (*browserlogin.WebLoginResult, error) {
		calledWith = provider
		return &browserlogin.WebLoginResult{
			CookieHeader: "sso=abc",
			Tokens:       map[string]string{"sso": "abc"},
			UserAgent:    "UA",
		}, nil
	}
	t.Cleanup(func() { providerWebBrowserLoginRun = prev })

	req := httptest.NewRequest(http.MethodPost, "/api/providers/grok-web/browser-login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if calledWith != "grok-web" {
		t.Errorf("runner called with %q", calledWith)
	}
	if !strings.Contains(rec.Body.String(), `"cookie":"sso=abc"`) {
		t.Errorf("cookie missing: %s", rec.Body.String())
	}
}

// The import-chrome generic path reuses the browser-cookie reader for the
// config's domain; the happy path answers the same shape the dedicated
// imports answer.
func TestProviderWebImportChrome(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)
	router := chi.NewRouter()
	router.Post("/api/providers/{id}/import-chrome", h.HandleProviderWebImportChrome)

	// Stand in for the cookie DB lookup.
	prevFind, prevRead := browserCookiesFindDB, browserCookiesReadDomain
	browserCookiesFindDB = func(string) (string, error) { return "/a-profile/Default/Cookies", nil }
	browserCookiesReadDomain = func(_ context.Context, _ string, _ string) (*browsercookies.Result, error) {
		return &browsercookies.Result{Header: "BDUSS=xyz"}, nil
	}
	t.Cleanup(func() { browserCookiesFindDB, browserCookiesReadDomain = prevFind, prevRead })

	req := httptest.NewRequest(http.MethodPost, "/api/providers/muse-web/import-chrome", strings.NewReader(`{"profile":""}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"cookie":"BDUSS=xyz"`) {
		t.Errorf("cookie missing: %s", rec.Body.String())
	}
}
