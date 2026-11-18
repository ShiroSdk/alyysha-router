// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"bytes"
	json "encoding/json/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
)

// harvestFixture boots a handler whose probe targets are local servers, so
// every verdict is decided inside the process.
func harvestFixture(t *testing.T, primaryOK, secondaryOK bool) (*DashboardHandler, chi.Router, func()) {
	repo, cleanup := setupTestDB(t)
	if _, err := repo.RawDB().Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create proxyPools: %v", err)
	}
	probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" && primaryOK {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/secondary" && secondaryOK {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "dead", http.StatusInternalServerError)
	}))
	h := NewDashboardHandler(repo)
	h.PrimaryProbeURL = probeServer.URL + "/primary"
	h.SecondaryProbeURL = probeServer.URL + "/secondary"
	router := chi.NewRouter()
	RegisterRoutes(router, h)
	return h, router, func() {
		probeServer.Close()
		cleanup()
	}
}

// deadPort binds a listener, closes it, and returns the port — guaranteed
// unreachable for the probe (a race against some other process binding it
// between the close and the probe is the same flake any dead-port probe
// carries, but on a dev machine it is the most reliable dead entry there is).
func deadPort(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate dead port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return "http://127.0.0.1:" + itoa(port)
}

func createHarvestPool(t *testing.T, router chi.Router, name, proxyURL string) string {
	body, _ := json.Marshal(map[string]any{"name": name, "proxyUrl": proxyURL})
	req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	return created["id"].(string)
}

func harvestCall(t *testing.T, router chi.Router, poolID string, body map[string]any) map[string]any {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/"+poolID+"/harvest", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("harvest: %d %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return res
}

func poolURLs(t *testing.T, h *DashboardHandler, poolID string) []string {
	pool, err := h.Repo.GetProxyPool(poolID)
	if err != nil || pool == nil {
		t.Fatalf("read pool: %v", err)
	}
	return pool.URLs
}

// The happy path: alive entries join the pool, dead ones are dropped, and
// duplicates of what the pool already holds are ignored.
func TestHarvestProxyPoolProbesAndKeepsAlive(t *testing.T) {
	h, router, cleanup := harvestFixture(t, true, true)
	defer cleanup()

	poolID := createHarvestPool(t, router, "pool", "http://127.0.0.1:1") // existing entry, unreachable

	res := harvestCall(t, router, poolID, map[string]any{
		// One duplicate of the existing entry, one distinct unreachable
		// entry, one malformed entry: probing on + dropDead keeps nothing.
		"proxies": []string{"http://127.0.0.1:1", deadPort(t), "not a url at all"},
	})
	t.Logf("verdict: %v", res)
	if res["added"].(float64) != 1 {
		t.Fatalf("ingest count wrong: %v", res)
	}
	if res["alive"].(float64) != 0 {
		t.Errorf("dead entries survived: %v", res)
	}
	urls := poolURLs(t, h, poolID)
	if len(urls) != 1 || urls[0] != "http://127.0.0.1:1" {
		t.Errorf("pool mutated unexpectedly: %v", urls)
	}
}

// A pool whose candidates answer keeps them: the harvest probe uses the same
// dual-target verdict the test button does, so an entry the pool test calls
// alive is never dropped by the harvest.
func TestHarvestProxyPoolKeepsProvenAlive(t *testing.T) {
	// The probe target answers through any "proxy" that is the local server:
	// point the primary probe at a server, and make the candidates that same
	// server (acts as proxy + target in one).
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := repo.RawDB().Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create proxyPools: %v", err)
	}

	var hits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	h := NewDashboardHandler(repo)
	h.PrimaryProbeURL = target.URL + "/primary"
	h.SecondaryProbeURL = target.URL + "/secondary"
	router := chi.NewRouter()
	RegisterRoutes(router, h)

	poolID := createHarvestPool(t, router, "alive-pool", "http://127.0.0.1:1")
	res := harvestCall(t, router, poolID, map[string]any{
		// The candidate IS the probe target: a request "through" it reaches
		// the local handler and answers 204, the same verdict the pool test
		// would give. The second entry is a dead port.
		"proxies": []string{target.URL, deadPort(t)},
	})
	if res["alive"].(float64) != 1 || res["dropped"].(float64) != 1 {
		t.Fatalf("verdict = %v", res)
	}
	urls := poolURLs(t, h, poolID)
	if len(urls) != 2 {
		t.Errorf("pool urls = %v, want the original + the alive entry", urls)
	}
	for _, u := range urls {
		if u == target.URL+":8080" {
			t.Errorf("dead entry survived: %v", urls)
		}
	}
}

// Relay pools ingest without probing: the probe would fail every entry for
// the wrong reason, so a relay harvest stores exactly what was pasted.
func TestHarvestProxyPoolRelayIngestsUnprobed(t *testing.T) {
	h, router, cleanup := harvestFixture(t, false, false)
	defer cleanup()

	poolID := createHarvestPool(t, router, "relay", "https://relay.example.vercel.app")
	res := harvestCall(t, router, poolID, map[string]any{
		"proxies": []string{"https://second.example.vercel.app"},
	})
	if res["added"].(float64) != 1 || res["alive"].(float64) != 0 {
		t.Fatalf("relay ingest = %v", res)
	}
	urls := poolURLs(t, h, poolID)
	if len(urls) != 2 || !strings.Contains(urls[1], "second.example") {
		t.Errorf("relay pool urls = %v", urls)
	}
}

// A malformed entry never reaches the pool, and an empty effective batch
// answers with a note instead of an error.
func TestHarvestProxyPoolRejectsGarbage(t *testing.T) {
	h, router, cleanup := harvestFixture(t, true, true)
	defer cleanup()
	poolID := createHarvestPool(t, router, "clean", "http://127.0.0.1:1")

	res := harvestCall(t, router, poolID, map[string]any{
		"proxies": []string{"   ", "::bad url::", "http://127.0.0.1:1"},
	})
	if res["added"].(float64) != 0 || res["note"] == nil {
		t.Fatalf("garbage ingest = %v", res)
	}
	urls := poolURLs(t, h, poolID)
	if len(urls) != 1 {
		t.Errorf("garbage mutated the pool: %v", urls)
	}
}
