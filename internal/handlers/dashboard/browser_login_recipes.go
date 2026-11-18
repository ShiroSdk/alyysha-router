// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"context"
	"errors"
	"net/http"

	"9routerx/proxy/internal/browserlogin"
	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/log"
)

// Browser login for the web-cookie providers (N12): the same throwaway
// browser flow gemini-web and deepseek-web use, pointed at three more
// providers. The answer is the credential the caller pastes into the
// connection form — it is never stored here; the caller decides which
// connection it belongs to. The runner is a variable so tests can answer
// without a browser, exactly like the gemini flow's.

var browserLoginRecipe = browserlogin.RunRecipe

// runBrowserRecipe handles one provider's flow and maps the shared failure
// modes to the same statuses the gemini flow answers with: no browser is a
// 501 (the machine cannot do it), a timeout is a 504, everything else a 400
// naming what did not happen.
func runBrowserRecipe(w http.ResponseWriter, r *http.Request, provider string, recipe browserlogin.Recipe, field string) {
	ctx, cancel := context.WithTimeout(r.Context(), browserLoginTimeout)
	defer cancel()

	log.Info(provider, "opening a browser for the sign-in")
	result, err := browserLoginRecipe(ctx, recipe)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, browserlogin.ErrNoBrowser):
			status = http.StatusNotImplemented
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		}
		log.Warn(provider, "the browser sign-in did not finish", "error", err)
		handlerutil.WriteJSONError(w, status, err.Error())
		return
	}

	log.Info(provider, "the browser sign-in finished")
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		field: result.CookieHeader,
	})
}

// HandleGrokWebBrowserLogin handles POST /api/providers/grok-web/browser-login.
func (h *DashboardHandler) HandleGrokWebBrowserLogin(w http.ResponseWriter, r *http.Request) {
	runBrowserRecipe(w, r, "grok-web", browserlogin.RecipeGrokWeb, "cookie")
}

// HandlePerplexityWebBrowserLogin handles POST /api/providers/perplexity-web/browser-login.
func (h *DashboardHandler) HandlePerplexityWebBrowserLogin(w http.ResponseWriter, r *http.Request) {
	runBrowserRecipe(w, r, "perplexity-web", browserlogin.RecipePerplexityWeb, "cookie")
}

// HandleClaudeWebBrowserLogin handles POST /api/providers/claude-web/browser-login.
// The provider stays opt-in: this flow only captures the cookie, the
// connection still refuses to serve while CLAUDE_WEB_ENABLED is off.
func (h *DashboardHandler) HandleClaudeWebBrowserLogin(w http.ResponseWriter, r *http.Request) {
	runBrowserRecipe(w, r, "claude-web", browserlogin.RecipeClaudeWeb, "cookie")
}
