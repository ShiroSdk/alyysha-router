// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9routerx/proxy/internal/browserlogin"
)

// withFakeBrowserLogin swaps the recipe runner for a stub and restores it.
func withFakeBrowserLogin(t *testing.T, fn func(ctx context.Context, recipe browserlogin.Recipe) (*browserlogin.Result, error)) {
	t.Helper()
	orig := browserLoginRecipe
	browserLoginRecipe = fn
	t.Cleanup(func() { browserLoginRecipe = orig })
}

func TestBrowserLoginRecipesCaptureTheSession(t *testing.T) {
	recipes := []struct {
		handler func(w http.ResponseWriter, r *http.Request)
		name    string
	}{
		{func(w http.ResponseWriter, r *http.Request) {
			runBrowserRecipe(w, r, "grok-web", browserlogin.RecipeGrokWeb, "cookie")
		}, "grok-web"},
		{func(w http.ResponseWriter, r *http.Request) {
			runBrowserRecipe(w, r, "perplexity-web", browserlogin.RecipePerplexityWeb, "cookie")
		}, "perplexity-web"},
		{func(w http.ResponseWriter, r *http.Request) {
			runBrowserRecipe(w, r, "claude-web", browserlogin.RecipeClaudeWeb, "cookie")
		}, "claude-web"},
	}
	for _, tc := range recipes {
		t.Run(tc.name, func(t *testing.T) {
			withFakeBrowserLogin(t, func(ctx context.Context, recipe browserlogin.Recipe) (*browserlogin.Result, error) {
				return &browserlogin.Result{CookieHeader: "session=1; other=2"}, nil
			})
			req := httptest.NewRequest(http.MethodPost, "/browser-login", nil)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"cookie":"session=1; other=2"`) {
				t.Errorf("body = %s", rec.Body.String())
			}
		})
	}
}

func TestBrowserLoginErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"no browser", browserlogin.ErrNoBrowser, http.StatusNotImplemented},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"no sign-in", browserlogin.ErrNoSignIn, http.StatusBadRequest},
		{"generic", errors.New("browser crashed"), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFakeBrowserLogin(t, func(ctx context.Context, recipe browserlogin.Recipe) (*browserlogin.Result, error) {
				return nil, tc.err
			})
			req := httptest.NewRequest(http.MethodPost, "/browser-login", nil)
			rec := httptest.NewRecorder()
			runBrowserRecipe(rec, req, "grok-web", browserlogin.RecipeGrokWeb, "cookie")
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestBrowserLoginRecipeDefinitions pins the cookie-domain and required-name
// triples the flows depend on: a renamed cookie upstream silently breaks the
// readiness check, so the definition is pinned here where the fix is obvious.
func TestBrowserLoginRecipeDefinitions(t *testing.T) {
	cases := []struct {
		recipe browserlogin.Recipe
		url    string
		domain string
		reqd   []string
	}{
		{browserlogin.RecipeGrokWeb, "https://grok.com/", "grok.com", []string{"sso"}},
		{browserlogin.RecipePerplexityWeb, "https://www.perplexity.ai/", "perplexity.ai", []string{"__Secure-next-auth.session-token"}},
		{browserlogin.RecipeClaudeWeb, "https://claude.ai/", "claude.ai", []string{"sessionKey"}},
		{browserlogin.RecipeGemini, "https://gemini.google.com/app", "google.com", []string{"__Secure-1PSID", "__Secure-1PSIDTS"}},
	}
	for _, tc := range cases {
		if tc.recipe.URL != tc.url || tc.recipe.CookieDomain != tc.domain {
			t.Errorf("recipe %s: url=%q domain=%q", tc.recipe.Name, tc.recipe.URL, tc.recipe.CookieDomain)
		}
		if strings.Join(tc.recipe.Required, ",") != strings.Join(tc.reqd, ",") {
			t.Errorf("recipe %s: required=%v, want %v", tc.recipe.Name, tc.recipe.Required, tc.reqd)
		}
	}
}
