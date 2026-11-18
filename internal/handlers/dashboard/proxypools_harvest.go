// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	json "encoding/json/v2"

	"9routerx/proxy/internal/handlerutil"
)

// Proxy pool harvest (N14): paste a batch of candidate proxies, and the pool
// absorbs the ones that are actually alive. The flow is ingest, probe, keep:
// entries are normalized (scheme defaulted, trimmed, deduplicated against the
// pool and each other), probed concurrently through the same dual-target
// probe the pool test uses, and only the survivors are written back. A batch
// that is never probed is never stored — a pool full of unproven entries
// turns every rotation step into a user-visible failure.

// harvestMaxBatch bounds one harvest call. A pool's rotation is only as fast
// as its dead entries are slow, so a five-hundred-entry batch is already a
// lot of pool to maintain by hand.
const harvestMaxBatch = 500

// harvestProbeWorkers probes candidates concurrently; eight keeps a batch
// bounded in wall-clock without stampeding the operator's egress IP.
const harvestProbeWorkers = 8

// HandleHarvestProxyPool handles POST /api/proxy-pools/{id}/harvest
// {proxies: string[], probe?: bool, dropDead?: bool}.
func (h *DashboardHandler) HandleHarvestProxyPool(w http.ResponseWriter, r *http.Request) {
	id := getURLParam(r, "id")
	if id == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "missing proxy pool id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var req struct {
		Proxies  []string `json:"proxies"`
		Probe    *bool    `json:"probe"`
		DropDead *bool    `json:"dropDead"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.Proxies) == 0 {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "proxies list is empty")
		return
	}
	if len(req.Proxies) > harvestMaxBatch {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "proxies list exceeds "+itoa(harvestMaxBatch)+" entries")
		return
	}
	probe := true
	if req.Probe != nil {
		probe = *req.Probe
	}
	dropDead := true
	if req.DropDead != nil {
		dropDead = *req.DropDead
	}

	pool, err := h.Repo.GetProxyPool(id)
	if err != nil || pool == nil {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "proxy pool not found")
		return
	}
	// An edge relay is not a dialable proxy: the probe would fail every
	// entry for the wrong reason, so relays only ingest, never probe.
	if pool.IsEdgeRelay() {
		probe = false
	}

	existing := make(map[string]bool, len(pool.URLs))
	for _, u := range pool.URLs {
		existing[normalizeProxyCandidate(u)] = true
	}

	added := make([]string, 0, len(req.Proxies))
	seen := make(map[string]bool, len(req.Proxies))
	for _, raw := range req.Proxies {
		normalized := normalizeProxyCandidate(raw)
		if normalized == "" || existing[normalized] || seen[normalized] {
			continue
		}
		seen[normalized] = true
		added = append(added, normalized)
	}
	if len(added) == 0 {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"added": 0, "alive": 0, "dropped": 0,
			"results": []map[string]any{},
			"note":    "every entry was a duplicate of what the pool already holds",
		})
		return
	}

	final := append(append([]string{}, pool.URLs...), added...)
	results := make([]map[string]any, 0, len(added))
	if probe {
		// alive scores EVERY added entry, true or false; the membership test
		// below must walk this map, not a filtered copy of the survivors, or
		// a dead entry fails the "scored" check and falls through to keep.
		alive := probeCandidates(r.Context(), h, added)
		kept := make([]string, 0, len(final))
		for _, u := range final {
			if scored, wasScored := alive[u]; wasScored {
				if scored || !dropDead {
					kept = append(kept, u)
				}
				continue
			}
			// Existing entries the probe never scored (every added entry is
			// scored, so this is the pool's original members) stay put; the
			// harvest does not silently prune what it was not asked to.
			kept = append(kept, u)
		}
		final = kept
		for _, u := range added {
			status := "dead"
			if alive[u] {
				status = "alive"
			}
			results = append(results, map[string]any{"url": u, "status": status})
		}
	}

	if err := h.Repo.UpdateProxyPool(id, map[string]any{"urls": final}); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	aliveCount := 0
	dropped := 0
	for _, res := range results {
		if res["status"] == "alive" {
			aliveCount++
		} else {
			dropped++
		}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"added":    len(added),
		"alive":    aliveCount,
		"dropped":  dropped,
		"results":  results,
		"poolSize": len(final),
	})
}

// normalizeProxyCandidate trims a pasted entry, defaults a missing scheme to
// http://, and rejects anything that is not a parseable URL with a host. The
// normalized spelling is the dedupe key, so whitespace and a missing scheme
// do not smuggle a duplicate past the ingest.
func normalizeProxyCandidate(raw string) string {
	entry := strings.TrimSpace(raw)
	entry = strings.Trim(entry, ",;")
	if entry == "" {
		return ""
	}
	if !strings.Contains(entry, "://") {
		entry = "http://" + entry
	}
	parsed, err := url.Parse(entry)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return entry
}

// probeCandidates scores the batch concurrently through the pool test's own
// dual-target probe, so a harvest verdict agrees with the verdict the test
// button would give the same entry.
func probeCandidates(ctx context.Context, h *DashboardHandler, candidates []string) map[string]bool {
	out := make(map[string]bool, len(candidates))
	var mu sync.Mutex
	jobs := make(chan string)
	var wg sync.WaitGroup
	workerCtx, cancel := context.WithTimeout(ctx, harvestProbeTimeout)
	defer cancel()
	for i := 0; i < harvestProbeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				ok := probeOneCandidate(workerCtx, h, candidate)
				mu.Lock()
				out[candidate] = ok
				mu.Unlock()
			}
		}()
	}
	for _, c := range candidates {
		jobs <- c
	}
	close(jobs)
	wg.Wait()
	return out
}

// harvestProbeTimeout bounds one candidate's dual probe; it mirrors the pool
// test's client timeout with slack for the fallback attempt.
const harvestProbeTimeout = 12 * time.Second

func probeOneCandidate(ctx context.Context, h *DashboardHandler, candidate string) bool {
	parsed, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	tr := &http.Transport{
		Proxy:             http.ProxyURL(parsed),
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	if ok, _, _ := probeViaProxy(ctx, client, h.primaryProbeURL()); ok {
		return true
	}
	ok, _, _ := probeViaProxy(ctx, client, h.secondaryProbeURL())
	return ok
}
