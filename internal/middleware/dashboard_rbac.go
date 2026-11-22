// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package middleware

import (
	"net/http"
	"sync"
	"time"

	"alyysha/proxy/internal/db"
	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/keikey"
)

// Dashboard access tokens (N5 RBAC): operator-minted bearer credentials that
// carry one scope — admin, write or read — and are enforced by the dashboard
// gates per route and method, failing closed. A presented token that is
// unknown, revoked or out of scope never degrades to the anonymous or
// no-login path; the gate answers 401 (unknown/revoked) or 403 (out of
// scope) and stops there.
//
// admin grants everything a dashboard session does, the always-protected and
// destructive surfaces included. write covers the management API but not the
// surfaces that hand out secrets or tear the install down. read is GET-only
// observability, refused outright on anything else — the fail-closed rule
// the task names: a read token can never write, no matter the route.

const DashboardAccessTokenHeader = "x-9r-access-token"

// tokenResolutionTTL bounds how long a resolved token stays trusted. A
// revocation takes up to this long to bite in a live process; Argon2id is
// far too heavy to pay on every dashboard poll, so the trade is explicit.
const tokenResolutionTTL = 30 * time.Second

type resolvedToken struct {
	scope    string
	expireAt time.Time
}

var (
	tokenCacheMu sync.Mutex
	tokenCache   = map[string]resolvedToken{}
)

// adminOnlyDashboardPaths are the surfaces a non-admin scope never reaches:
// the destructive and secret-material surfaces the always-protected list and
// the export endpoints cover. Everything else in the management API is in
// scope for write, and its GETs for read.
var adminOnlyDashboardPaths = map[string]bool{
	PasswordHeaderCarriesOwnAuth:    true, // the backup download hands out the whole database
	"/api/settings/database/import": true,
}

// isSecretMaterialRead matches the reads that hand out credentials: the key
// list carries plaintext for legacy rows, and the provider export hands out
// upstream keys. A read is admin-only; a write to those same routes is
// already refused to the read scope by the method check.
func isSecretMaterialRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	return path == "/api/keys" || isExportKeysPath(path)
}

// AccessTokenScope resolves the presented access token to its scope. The
// second return is false when no token header is present, when the token is
// unknown, or when it has been revoked.
func AccessTokenScope(repo *db.Repo, r *http.Request) (string, bool) {
	plaintext := r.Header.Get(DashboardAccessTokenHeader)
	if plaintext == "" || repo == nil {
		return "", false
	}
	lookup := lookupFor(plaintext)
	tokenCacheMu.Lock()
	if hit, ok := tokenCache[lookup]; ok && time.Now().Before(hit.expireAt) {
		tokenCacheMu.Unlock()
		return hit.scope, true
	}
	tokenCacheMu.Unlock()

	tok, err := repo.ResolveDashboardToken(r.Context(), plaintext)
	if err != nil || tok == nil {
		return "", false
	}
	tokenCacheMu.Lock()
	// Bound the cache the same way the ledger does: past the cap, drop it
	// wholesale rather than grow. Dashboard tokens number in the single
	// digits on any real install.
	if len(tokenCache) >= 256 {
		tokenCache = map[string]resolvedToken{}
	}
	tokenCache[lookup] = resolvedToken{scope: tok.Scope, expireAt: time.Now().Add(tokenResolutionTTL)}
	tokenCacheMu.Unlock()
	return tok.Scope, true
}

// InvalidateDashboardTokenCache drops the resolution cache, so a revocation
// (or a mint) takes effect immediately on the instance that did it. Tests
// use it too.
func InvalidateDashboardTokenCache() {
	tokenCacheMu.Lock()
	tokenCache = map[string]resolvedToken{}
	tokenCacheMu.Unlock()
}

// lookupFor hashes the presented plaintext for the cache key. The plaintext
// itself must never sit in a process-wide map; the fast hash is the same one
// the row is indexed by, reused so the cache key cannot collide across
// token generations.
func lookupFor(plaintext string) string {
	return keikey.LookupHash(plaintext)
}

// AccessTokenScopeAllows reports whether the scope may proceed on this
// method and path. The unknown scope is denied, which is the fail-closed
// default: a scope the gate does not know is a scope that grants nothing.
func AccessTokenScopeAllows(scope, method, path string) bool {
	switch scope {
	case db.DashboardTokenScopeAdmin:
		return true
	case db.DashboardTokenScopeWrite:
		if isSecretMaterialRead(method, path) {
			return false
		}
		return !IsAlwaysProtectedPath(path) && !adminOnlyDashboardPaths[path] && !isExportKeysPath(path)
	case db.DashboardTokenScopeRead:
		if method != http.MethodGet {
			// Fail-closed for write: a read token answers 403 before the
			// route's handler runs, whatever the route is.
			return false
		}
		if isSecretMaterialRead(method, path) {
			return false
		}
		return !IsAlwaysProtectedPath(path) && !adminOnlyDashboardPaths[path] && !isExportKeysPath(path)
	}
	return false
}

// isExportKeysPath matches the provider key export — the one templated route
// that hands out upstream credentials.
func isExportKeysPath(path string) bool {
	const suffix = "/export-keys"
	return len(path) > len(suffix) && path[len(path)-len(suffix):] == suffix
}

// requireDashboardAccessToken is the shared gate body: when the request
// carries an access token, its scope decides everything and the response is
// final — including serving next on the allowed path, because falling back
// to the caller's session check would turn a valid token into a 401. Returns
// false only when the request has no token at all, in which case the caller's
// own credential checks run as before.
func requireDashboardAccessToken(repo *db.Repo, w http.ResponseWriter, r *http.Request, adminOnly bool, next http.Handler) bool {
	if r.Header.Get(DashboardAccessTokenHeader) == "" {
		return false
	}
	scope, ok := AccessTokenScope(repo, r)
	if !ok {
		handlerutil.WriteJSONError(w, http.StatusUnauthorized, "Unauthorized: invalid access token")
		return true
	}
	if adminOnly && scope != db.DashboardTokenScopeAdmin {
		handlerutil.WriteJSONErrorCode(w, http.StatusForbidden, "forbidden", "Admin scope required")
		return true
	}
	if !AccessTokenScopeAllows(scope, r.Method, r.URL.Path) {
		handlerutil.WriteJSONErrorCode(w, http.StatusForbidden, "forbidden", "Token scope does not allow this route")
		return true
	}
	next.ServeHTTP(w, r)
	return true
}
