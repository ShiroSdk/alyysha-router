// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"9routerx/proxy/internal/db"
	"9routerx/proxy/internal/keikey"
)

// mintToken creates one live token through the repo and returns its
// plaintext, exercising the same primitives a mint request would.
func mintToken(t *testing.T, repo *db.Repo, plaintext, scope string) {
	t.Helper()
	hash, err := keikey.Hash(plaintext)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := repo.CreateDashboardToken(context.Background(), "dtok_"+plaintext[len(plaintext)-6:], "test", hash, keikey.LookupHash(plaintext), scope); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// TestAccessTokenScopeMatrix pins the route/method rules each scope obeys.
func TestAccessTokenScopeMatrix(t *testing.T) {
	cases := []struct {
		scope  string
		method string
		path   string
		want   bool
	}{
		{"admin", http.MethodGet, "/api/keys", true},
		{"admin", http.MethodPost, "/api/shutdown", true},
		{"admin", http.MethodGet, "/api/settings/database", true},
		{"write", http.MethodPost, "/api/keys", true},
		{"write", http.MethodGet, "/api/combos", true},
		{"write", http.MethodPost, "/api/shutdown", false},
		{"write", http.MethodGet, "/api/settings/database", false},
		{"write", http.MethodGet, "/api/providers/x/export-keys", false},
		{"read", http.MethodGet, "/api/combos", true},
		{"read", http.MethodGet, "/api/burn-rate", true},
		{"read", http.MethodPost, "/api/keys", false},
		{"read", http.MethodPut, "/api/combos/x", false},
		{"read", http.MethodDelete, "/api/keys/x", false},
		{"read", http.MethodGet, "/api/keys", false},
		{"read", http.MethodGet, "/api/shutdown", false},
		{"nonsense", http.MethodGet, "/api/keys", false},
	}
	for _, tc := range cases {
		if got := AccessTokenScopeAllows(tc.scope, tc.method, tc.path); got != tc.want {
			t.Errorf("scope=%s %s %s = %v, want %v", tc.scope, tc.method, tc.path, got, tc.want)
		}
	}
}

// TestAccessTokenGateFailClosed proves the gate never degrades a presented
// token to the anonymous path: unknown tokens answer 401 and stop, valid
// tokens pass only within scope, and out-of-scope writes are refused before
// any handler runs.
func TestAccessTokenGateFailClosed(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)

	plainWrite := "9rdt_gate_write000"
	plainRead := "9rdt_gate_read0000"
	mintToken(t, repo, plainWrite, "write")
	mintToken(t, repo, plainRead, "read")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("served"))
	})
	gate := RequireDashboardAuth(repo)

	run := func(token, method, path string) int {
		InvalidateDashboardTokenCache()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set(DashboardAccessTokenHeader, token)
		}
		rec := httptest.NewRecorder()
		gate(handler).ServeHTTP(rec, req)
		return rec.Code
	}

	// A valid write token serves the management write it is scoped for.
	if code := run(plainWrite, http.MethodPost, "/api/keys"); code != http.StatusOK {
		t.Errorf("write token on /api/keys: %d, want 200", code)
	}
	// The same token is refused on the destructive surface before any handler.
	if code := run(plainWrite, http.MethodPost, "/api/shutdown"); code != http.StatusForbidden {
		t.Errorf("write token on /api/shutdown: %d, want 403", code)
	}
	// The read token is fail-closed on writes even on routes it can read.
	if code := run(plainRead, http.MethodGet, "/api/combos"); code != http.StatusOK {
		t.Errorf("read token on GET /api/combos: %d, want 200", code)
	}
	if code := run(plainRead, http.MethodDelete, "/api/cache"); code != http.StatusForbidden {
		t.Errorf("read token on DELETE: %d, want 403", code)
	}
	// An unknown token must not ride the no-login path: it answers 401 and
	// stops, whatever requireLogin says.
	if code := run("9rdt_gate_unknown", http.MethodGet, "/api/keys"); code != http.StatusUnauthorized {
		t.Errorf("unknown token: %d, want 401", code)
	}
}
