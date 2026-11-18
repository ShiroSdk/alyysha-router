// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	json "encoding/json/v2"

	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/log"
	"9routerx/proxy/internal/models"
	"9routerx/proxy/internal/providers"
)

// Model arena (N18b): run one prompt against several of the operator's own
// connections side by side, so a model choice can be compared on real output
// instead of vibes. The dispatch is deliberately narrow for v1: API-key and
// compatible connections are dialed directly with a minimal Chat Completions
// request; OAuth and web-cookie members are reported as skipped with the
// reason — their credential flows belong to the engine's refresh machinery,
// and an arena that half-faked them would compare nothing.

// arenaMaxMembers bounds one run: four to six models side by side is the
// comparison a human reads; more is a benchmark, not an arena.
const arenaMaxMembers = 6

// arenaRequestTimeout bounds one member's completion.
const arenaRequestTimeout = 90 * time.Second

// arenaMaxWorkers dials four members at once.
const arenaMaxWorkers = 4

type arenaResult struct {
	Member    string `json:"member"`
	Model     string `json:"model,omitempty"`
	Content   string `json:"content,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Tokens    int64  `json:"tokens,omitempty"`
	Error     string `json:"error,omitempty"`
	Skipped   string `json:"skipped,omitempty"`
}

// HandleArenaRun handles POST /api/arena/run {prompt, members, maxTokens?}.
func (h *DashboardHandler) HandleArenaRun(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()
	var req struct {
		Prompt    string   `json:"prompt"`
		Members   []string `json:"members"`
		MaxTokens int      `json:"maxTokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "prompt is empty")
		return
	}
	if len(req.Members) == 0 || len(req.Members) > arenaMaxMembers {
		handlerutil.WriteJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("members must be between 1 and %d", arenaMaxMembers))
		return
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 || maxTokens > 2048 {
		maxTokens = 256
	}

	results := make([]arenaResult, len(req.Members))
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan int)
	runCtx, cancel := context.WithTimeout(r.Context(), arenaRequestTimeout)
	defer cancel()
	for worker := 0; worker < arenaMaxWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				res := h.arenaRunOne(runCtx, req.Members[idx], req.Prompt, maxTokens)
				mu.Lock()
				results[idx] = res
				mu.Unlock()
			}
		}()
	}
	for i := range req.Members {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"prompt":  req.Prompt,
		"ranAt":   time.Now().UTC().Format(time.RFC3339),
	})
}

// arenaRunOne dispatches one member. The member spelling is the combo
// member's "alias/model"; the alias resolves to an active connection whose
// stored credential and base URL carry the call.
func (h *DashboardHandler) arenaRunOne(ctx context.Context, member, prompt string, maxTokens int) arenaResult {
	res := arenaResult{Member: member}
	slash := strings.LastIndex(member, "/")
	if slash < 0 || slash == len(member)-1 {
		res.Skipped = "member is not an alias/model pair"
		return res
	}
	alias := member[:slash]
	model := member[slash+1:]
	res.Model = model

	connections, err := h.Repo.GetProviderConnections(alias, true)
	if err != nil || len(connections) == 0 {
		res.Skipped = "no active connection for " + alias
		return res
	}
	conn := connections[0]

	raw, _ := decodeConnectionData(conn.Data)
	if raw == nil {
		raw = map[string]any{}
	}
	data := parseConnectionProbeData(raw)
	base := h.arenaBaseURL(alias, conn, data, raw)
	credential, kind := h.arenaCredential(conn, data)
	switch kind {
	case arenaAuthAPIKey:
	case arenaAuthOAuth, arenaAuthCookie:
		res.Skipped = fmt.Sprintf("%s uses %s auth; the arena v1 dials API-key connections only", alias, kind)
		return res
	default:
		res.Skipped = "no usable credential on the connection"
		return res
	}
	if base == "" {
		res.Skipped = "connection carries no base URL"
		return res
	}

	payload, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, arenaChatEndpoint(base), strings.NewReader(string(payload)))
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: arenaRequestTimeout}
	start := time.Now()
	resp, err := client.Do(req)
	res.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(respBody))
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		res.Error = fmt.Sprintf("upstream answered %d: %s", resp.StatusCode, snippet)
		return res
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || len(parsed.Choices) == 0 {
		res.Error = "unparseable completion body"
		return res
	}
	res.Content = parsed.Choices[0].Message.Content
	res.Tokens = parsed.Usage.TotalTokens
	log.Info("arena", "member answered", "member", member, "latency_ms", res.LatencyMs)
	return res
}

const (
	arenaAuthAPIKey   = "api-key"
	arenaAuthOAuth    = "oauth"
	arenaAuthCookie   = "web-cookie"
	arenaAuthNone     = "none"
	arenaAuthUnusable = "unusable"
)

// arenaCredential classifies the connection and returns what a direct call
// needs. Web-cookie and OAuth connections are named so the arena can skip
// them honestly instead of dialing with a credential shape they do not take.
func (h *DashboardHandler) arenaCredential(conn *models.ProviderConnection, data connectionProbeData) (string, string) {
	if conn.AuthType == "cookie" {
		return "", arenaAuthCookie
	}
	if data.APIKey != "" {
		return data.APIKey, arenaAuthAPIKey
	}
	if data.AccessToken != "" {
		return "", arenaAuthOAuth
	}
	return "", arenaAuthUnusable
}

// arenaBaseURL resolves the endpoint base: the connection's own baseUrl when
// it carries one, else the registry's BaseURL for a known provider.
func (h *DashboardHandler) arenaBaseURL(alias string, conn *models.ProviderConnection, data connectionProbeData, raw map[string]any) string {
	base := psdStr(data.ProviderSpecificData, "baseUrl")
	if base == "" {
		base = psdStr(raw, "baseUrl")
	}
	if base == "" {
		if _, nodeData, err := h.Repo.GetProviderNodeByID(conn.Provider); err == nil && nodeData != nil {
			base = strings.TrimSpace(nodeData.BaseURL)
		}
	}
	if base == "" {
		if cfg, known := providers.KnownProviders[providers.ResolveAlias(alias)]; known {
			base = cfg.BaseURL
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

// arenaChatEndpoint derives the Chat Completions endpoint from a base, the
// same /v1 handling the connection probe's URL derivation uses: a base that
// already ends in /v1 gets just the path appended.
func arenaChatEndpoint(base string) string {
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}
