// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"9routerx/proxy/internal/db"
)

// arenaFixture boots a handler with one active API-key connection pointing at
// the local upstream, so the arena's dial stays inside the process.
func arenaFixture(t *testing.T, upstream http.HandlerFunc) (*DashboardHandler, chi.Router, func()) {
	repo, cleanup := setupTestDB(t)
	if _, err := repo.RawDB().Exec(`CREATE TABLE IF NOT EXISTS providerConnections (
		id TEXT PRIMARY KEY,
		provider TEXT,
		authType TEXT,
		name TEXT,
		email TEXT,
		priority INTEGER,
		isActive INTEGER DEFAULT 1,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create providerConnections: %v", err)
	}
	upstreamSrv := httptest.NewServer(upstream)
	h := NewDashboardHandler(repo)
	router := chi.NewRouter()
	RegisterRoutes(router, h)

	if err := repo.CreateProviderConnectionFull("conn-arena", "openai-compatible-arena", "compatible", "arena", nil,
		`{"apiKey":"sk-arena","providerSpecificData":{"baseUrl":"`+upstreamSrv.URL+`"}}`); err != nil {
		t.Fatalf("create connection: %v", err)
	}
	return h, router, func() {
		upstreamSrv.Close()
		cleanup()
	}
}

func arenaCall(t *testing.T, router chi.Router, body map[string]any) map[string]any {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/arena/run", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("arena: %d %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return res
}

// The happy path: the member dials the connection's base URL, the completion
// text lands in the result, and the latency is recorded.
func TestArenaRunDialsMember(t *testing.T) {
	h, router, cleanup := arenaFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-arena" {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"arena says hi"}}],"usage":{"total_tokens":42}}`))
	})
	defer cleanup()

	res := arenaCall(t, router, map[string]any{
		"prompt":  "say hi",
		"members": []string{"openai-compatible-arena/arena-model"},
	})
	results := res["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	first := results[0].(map[string]any)
	if first["content"] != "arena says hi" {
		t.Errorf("content = %v", first["content"])
	}
	if first["tokens"].(float64) != 42 {
		t.Errorf("tokens = %v", first["tokens"])
	}
	if first["error"] != nil || first["skipped"] != nil {
		t.Errorf("unexpected failure: %v / %v", first["error"], first["skipped"])
	}
	_ = h
}

// OAuth and web-cookie members are skipped with the reason, not dialed with
// a credential shape they do not take.
func TestArenaRunSkipsNonAPIKeyMembers(t *testing.T) {
	dialed := 0
	h, router, cleanup := arenaFixture(t, func(w http.ResponseWriter, r *http.Request) {
		dialed++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	})
	defer cleanup()
	_ = h

	// A cookie-auth provider the arena knows: grok-web is CategoryWebCookie,
	// so the member resolves to a connection the arena must skip. The
	// connection does not exist, so the answer is the no-connection skip.
	res := arenaCall(t, router, map[string]any{
		"prompt":  "hi",
		"members": []string{"grok-web/grok-4", "openai-compatible-arena/missing-conn"},
	})
	results := res["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %d", len(results))
	}
	skipped := results[0].(map[string]any)["skipped"]
	if skipped == nil || skipped == "" {
		t.Errorf("web-cookie member was not skipped: %v", results[0])
	}
	// The compatible member resolves to the fixture connection and dials; the
	// cookie member must not.
	if dialed != 1 {
		t.Errorf("the arena dialed %d members, want exactly the API-key one", dialed)
	}
}

// Input guards: an empty prompt and an oversized member list are refused
// before any dial.
func TestArenaRunInputGuards(t *testing.T) {
	_, router, cleanup := arenaFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the arena dialed despite a refused request")
	})
	defer cleanup()

	payload, _ := json.Marshal(map[string]any{"prompt": "  ", "members": []string{"openai/x"}})
	req := httptest.NewRequest(http.MethodPost, "/api/arena/run", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty prompt = %d, want 400", rec.Code)
	}

	payload, _ = json.Marshal(map[string]any{"prompt": "hi", "members": []string{"a/x", "b/x", "c/x", "d/x", "e/x", "f/x", "g/x"}})
	req = httptest.NewRequest(http.MethodPost, "/api/arena/run", bytes.NewReader(payload))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized batch = %d, want 400", rec.Code)
	}
}

// The cache hit-rate block: percentages fold from the counters, and a window
// with no requests reads as zero rather than NaN.
func TestCacheHitRateFolds(t *testing.T) {
	if got := promptCacheHitRate(nil); got != 0 {
		t.Errorf("nil metrics rate = %v", got)
	}
	if got := promptCacheHitRate(&db.PromptCacheMetrics{}); got != 0 {
		t.Errorf("empty window rate = %v", got)
	}
	m := db.PromptCacheMetrics{TotalRequests: 8, RequestsWithCacheControl: 3}
	if got := promptCacheHitRate(&m); got != 37.5 {
		t.Errorf("rate = %v, want 37.5", got)
	}
	if got := semanticHitRate(0, 0); got != 0 {
		t.Errorf("empty semantic rate = %v", got)
	}
	if got := semanticHitRate(2, 6); got != 25 {
		t.Errorf("semantic rate = %v, want 25", got)
	}
}
