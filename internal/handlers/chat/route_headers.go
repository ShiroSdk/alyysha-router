// Alyysha Router — derived from the work of https://github.com/ShiroSdk/ — port and modifications by the maintainers — https://t.me/YannClippers
package chat

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"alyysha/proxy/internal/envcompat"
)

var (
	routeHeadersEnabledOnce sync.Once
	routeHeadersEnabledVal  bool
)

// isRouteHeadersEnabled checks if route response headers are enabled via ALYYSHA_ROUTE_HEADERS.
// Defaults to true. Returns false if set to "0" or "false" (case-insensitive).
// Cached after the first lookup to avoid per-request os.Getenv overhead.
func isRouteHeadersEnabled() bool {
	routeHeadersEnabledOnce.Do(func() {
		val := envcompat.Getenv("ALYYSHA_ROUTE_HEADERS", "NINEROUTER_ROUTE_HEADERS")
		if val == "" {
			routeHeadersEnabledVal = true
			return
		}
		low := strings.ToLower(val)
		routeHeadersEnabledVal = low != "0" && low != "false"
	})
	return routeHeadersEnabledVal
}

// resetRouteHeadersConfigForTesting allows unit tests to re-evaluate ALYYSHA_ROUTE_HEADERS.
func resetRouteHeadersConfigForTesting() {
	routeHeadersEnabledOnce = sync.Once{}
}

// injectRouteHeaders sets informational response headers indicating which provider,
// model, combo, and attempt count successfully served the request.
func injectRouteHeaders(w http.ResponseWriter, provider, model, combo string, attempts int) {
	if !isRouteHeadersEnabled() {
		return
	}
	if provider != "" {
		w.Header().Set("x-alyysha-provider", provider)
	}
	if model != "" {
		w.Header().Set("x-alyysha-model", model)
	}
	if combo != "" {
		w.Header().Set("x-alyysha-combo", combo)
	}
	if attempts <= 0 {
		attempts = 1
	}
	w.Header().Set("x-alyysha-attempts", strconv.Itoa(attempts))
}

// clearRouteHeaders removes route metadata headers from w. Used when a dispatch
// attempt fails so that headers from an unserved attempt do not leak into error responses.
func clearRouteHeaders(w http.ResponseWriter) {
	if w == nil {
		return
	}
	h := w.Header()
	h.Del("x-alyysha-provider")
	h.Del("x-alyysha-model")
	h.Del("x-alyysha-combo")
	h.Del("x-alyysha-attempts")
}

// attemptCount returns the number of fallback attempts recorded so far on ctx,
// plus one for the current (about-to-be-dispatched) attempt.
func attemptCount(ctx context.Context) int {
	rec := FallbackAttemptRecorderFrom(ctx)
	if rec == nil {
		return 1
	}
	snap := rec.Snapshot()
	return len(snap) + 1
}
