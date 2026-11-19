// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"9routerx/proxy/internal/proxy"
)

// pplxTestSetup resets the session ledger and points the endpoint at a fake
// upstream for one test. The fake also serves the probe target so the
// request through it lands inside the process.
func pplxTestSetup(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	SetWebExecutorsEnabled(true)
	t.Cleanup(func() { SetWebExecutorsEnabled(false) })
	pplxSessionReset()
	prevURL := perplexityWebChatURL
	server := httptest.NewServer(handler)
	perplexityWebChatURL = server.URL
	t.Cleanup(func() {
		perplexityWebChatURL = prevURL
		server.Close()
		pplxSessionReset()
	})
	return server
}

func pplxTestBody(model string, content string) []byte {
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]any{{"role": "user", "content": content}},
	})
	return body
}

func pplxTestConfig() *providersConfig {
	return &providersConfig{}
}

// providersConfig aliases the executor's config type for readability at the
// call sites below.
type providersConfig = providersConfigType

func pplxDo(t *testing.T, req *Request) (*httptest.ResponseRecorder, error) {
	rec := httptest.NewRecorder()
	err := ForwardPerplexityWeb(rec, req)
	return rec, err
}

func pplxReq(t *testing.T, body []byte, stream bool) *Request {
	return &Request{
		Ctx:      context.Background(),
		Body:     body,
		IsStream: stream,
		APIKey:   "test-session-token-value",
	}
}

func pplxSSEFrames(events ...string) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString("data: ")
		b.WriteString(ev)
		b.WriteString("\n\n")
	}
	b.WriteString(pplxStreamEOF + "\n\n")
	return b.String()
}

func TestPerplexityWebRegistered(t *testing.T) {
	if Get("perplexity-web") == nil {
		t.Fatal("perplexity-web is not registered")
	}
}

func TestPerplexityWebEmptyMessages(t *testing.T) {
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream dialed for an empty-messages request")
	})
	body, _ := json.Marshal(map[string]any{"model": "pplx-auto", "messages": []any{}})
	rec, err := pplxDo(t, pplxReq(t, body, false))
	if err == nil {
		t.Fatal("empty messages accepted")
	}
	var upstream *proxy.UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %v, want 400", err)
	}
	if !strings.Contains(rec.Body.String(), "") && rec.Code != 200 {
		t.Errorf("body noise: %s", rec.Body.String())
	}
}

func TestPerplexityWebRequiresCookie(t *testing.T) {
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream dialed without a cookie")
	})
	req := pplxReq(t, pplxTestBody("pplx-auto", "hi"), false)
	req.APIKey = ""
	rec, err := pplxDo(t, req)
	if err == nil {
		t.Fatal("missing cookie accepted")
	}
	var upstream *proxy.UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", err)
	}
	_ = rec
}

func TestPerplexityWebParseMessages(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "be helpful"},
		map[string]any{"role": "developer", "content": "no secrets"},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "hello "},
			map[string]any{"type": "text", "text": "world"},
		}},
		map[string]any{"role": "assistant", "content": "hi there"},
		map[string]any{"role": "user", "content": "again"},
		map[string]any{"role": "user", "content": "   "},
	}
	parsed := pplxParseOpenAIMessages(messages)
	if !strings.Contains(parsed.system, "be helpful") || !strings.Contains(parsed.system, "no secrets") {
		t.Errorf("system accumulation broken: %q", parsed.system)
	}
	if parsed.current != "again" {
		t.Errorf("current = %q, want the trailing non-empty user message", parsed.current)
	}
	found := false
	for _, h := range parsed.history {
		if strings.Contains(h.content, "hello") && strings.Contains(h.content, "world") {
			found = true
		}
	}
	if !found {
		t.Errorf("multi-part user content not joined: %+v", parsed.history)
	}
	if len(parsed.history) != 2 {
		t.Errorf("history = %+v, want hello-world + assistant", parsed.history)
	}
}

func TestPerplexityWebCleanResponse(t *testing.T) {
	in := "Answer[1] with [2][3] citations.\n\n<grok>foreign</grok>\n<?xml version=\"1.0\"?>\n<response>body</response>\n\ndone."
	out := pplxCleanResponse(in, true)
	if strings.Contains(out, "[1]") || strings.Contains(out, "<grok>") || strings.Contains(out, "<?xml") || strings.Contains(out, "<response>") {
		t.Errorf("artifacts survived: %q", out)
	}
	if !strings.HasPrefix(out, "Answer with citations.") {
		t.Errorf("cleaned = %q", out)
	}
	// strip=false keeps the body's whitespace (streaming path).
	kept := pplxCleanResponse("line1\n\n\tindented", false)
	if !strings.Contains(kept, "\n\n\tindented") {
		t.Errorf("strip=false damaged whitespace: %q", kept)
	}
}

func TestPerplexityWebQueryBudget(t *testing.T) {
	parsed := pplxParsedMessages{current: strings.Repeat("x", 400)}
	parsed.system = strings.Repeat("s", 100000)
	query := pplxBuildQuery(parsed, "", "")
	if len(query) != pplxQueryBudget {
		t.Fatalf("query len = %d, want %d", len(query), pplxQueryBudget)
	}
	// The tail is kept: the newest content (the JSON closing) survives.
	if !strings.HasSuffix(query, "}") {
		t.Errorf("tail not preserved: %q", query[len(query)-40:])
	}
}

func TestPerplexityWebSessionRoundTripAndTTL(t *testing.T) {
	pplxSessionReset()
	var history []pplxHistoryItem
	if got := pplxSessionLookupFn(history); got != "" {
		t.Fatalf("fresh history returned %q", got)
	}
	// Turn 1: history is empty and "first" is the current message; the store
	// key covers history + current + answer, which is exactly the history
	// the next turn parses.
	pplxSessionStoreFn(history, "first", "answer one", "backend-1")
	next := []pplxHistoryItem{
		{role: "user", content: "first"},
		{role: "assistant", content: "answer one"},
	}
	if got := pplxSessionLookupFn(next); got != "backend-1" {
		t.Fatalf("continuation lookup = %q, want backend-1", got)
	}
	// TTL: a clock push past the hour expires the entry.
	base := time.Now()
	pplxSessionClock = func() time.Time { return base.Add(pplxSessionTTL + time.Minute) }
	t.Cleanup(func() { pplxSessionClock = time.Now })
	if got := pplxSessionLookupFn(next); got != "" {
		t.Errorf("stale entry returned %q", got)
	}
}

func TestPerplexityWebExtractMarkdownDone(t *testing.T) {
	state := &pplxExtractState{}
	events := []map[string]any{
		{"backend_uuid": "bk-1", "usage": "ask_text_markdown_0", "markdown_block": map[string]any{"chunks": []any{"Hello "}}},
		{"block": map[string]any{"diff_block": map[string]any{"field": "workflow_block", "diff": []any{
			map[string]any{"path": "/chunks/0", "value": "Hello world from pplx"},
		}}}},
		{"status": "COMPLETED", "text": `{"step_type":"FINAL","content":{"answer":"{\"answer\":\"Hello world from pplx\"}"}}`},
	}
	var content string
	for _, ev := range events {
		delta, _, done, err := state.pplxExtractContent(ev)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		content += delta
		if done {
			break
		}
	}
	if content != "Hello world from pplx" {
		t.Errorf("content = %q", content)
	}
	if state.backendUUID != "bk-1" {
		t.Errorf("backend uuid = %q", state.backendUUID)
	}
	if state.fullAnswer != "Hello world from pplx" {
		t.Errorf("full answer = %q", state.fullAnswer)
	}
}

func TestPerplexityWebSearchThinking(t *testing.T) {
	state := &pplxExtractState{}
	thinking := state.extractThinking(map[string]any{
		"usage": "pro_search_steps",
		"block": map[string]any{"plan_block": map[string]any{"steps": []any{
			map[string]any{"step_type": "SEARCH_WEB", "query": "perplexity api"},
			map[string]any{"step_type": "SEARCH_WEB", "query": "perplexity api"},
			map[string]any{"step_type": "READ_RESULTS", "urls": []any{"https://a.example", "https://b.example", "https://c.example", "https://d.example"}},
		}}},
	})
	if !strings.Contains(thinking, "Searching: perplexity api") {
		t.Errorf("search step missing: %q", thinking)
	}
	if strings.Count(thinking, "Searching:") != 1 {
		t.Errorf("duplicate search step: %q", thinking)
	}
	if strings.Count(thinking, "Reading:") != 3 {
		t.Errorf("read steps = %q, want 3 urls max", thinking)
	}
}

func TestPerplexityWebUUIDShape(t *testing.T) {
	uid := pplxUUID()
	parts := strings.Split(uid, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[4]) != 12 {
		t.Errorf("uuid shape = %q", uid)
	}
	if !strings.Contains(uid, "4") {
		t.Errorf("not a v4 uuid: %q", uid)
	}
}

func TestPerplexityWebNonStream(t *testing.T) {
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "__Secure-next-auth.session-token=test-session-token-value" {
			t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("x-perplexity-request-endpoint") != perplexityWebSSEPath {
			t.Errorf("endpoint header missing")
		}
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		params, _ := sent["params"].(map[string]any)
		if params["mode"] != "copilot" || params["is_incognito"] != true || params["use_schematized_api"] != true {
			t.Errorf("params wrong: %v", params)
		}
		if params["version"] != pplxAPIVersion {
			t.Errorf("version = %v", params["version"])
		}
		if _, has := params["frontend_uuid"]; !has {
			t.Errorf("frontend_uuid missing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(pplxSSEFrames(
			`{"backend_uuid":"bk-9","usage":"ask_text_markdown_0","markdown_block":{"answer":"the answer"}}`,
			`{"status":"COMPLETED"}`,
		)))
	})
	req := pplxReq(t, pplxTestBody("pplx-auto", "hi"), false)
	rec, err := pplxDo(t, req)
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	var completion struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &completion); err != nil {
		t.Fatalf("parse: %v — body: %s", err, rec.Body.String())
	}
	if completion.ID == "" || !strings.HasPrefix(completion.ID, "chatcmpl-pplx-") {
		t.Errorf("id = %q", completion.ID)
	}
	if completion.Choices[0].Message.Content != "the answer" {
		t.Errorf("content = %q", completion.Choices[0].Message.Content)
	}
	if completion.Choices[0].FinishReason != "stop" {
		t.Errorf("finish = %q", completion.Choices[0].FinishReason)
	}
}

func TestPerplexityWebStream(t *testing.T) {
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond) // measurable TTFT
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(pplxSSEFrames(
			`{"backend_uuid":"bk-s","usage":"pro_search_steps","block":{"plan_block":{"steps":[{"step_type":"SEARCH_WEB","query":"weather"}]}}}`,
			`{"usage":"ask_text_markdown_0","markdown_block":{"chunks":["Today: sunny"]}}`,
			`{"status":"COMPLETED"}`,
		)))
	})
	req := pplxReq(t, pplxTestBody("pplx-auto", "weather?"), true)
	req.TTFT = new(int64)
	req.StartTime = time.Now() // the fake sleeps 5ms upstream-side
	rec, err := pplxDo(t, req)
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"reasoning_content"`) || !strings.Contains(body, "Searching: weather") {
		t.Errorf("thinking delta missing: %s", body)
	}
	if !strings.Contains(body, "sunny") {
		t.Errorf("content delta missing: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) || !strings.Contains(body, "data: [DONE]") {
		t.Errorf("terminator missing: %s", body)
	}
	if *req.TTFT == 0 {
		t.Errorf("TTFT not recorded")
	}
	// The thinking narration must precede the content deltas in wire order.
	if strings.Index(body, "reasoning_content") > strings.Index(body, "sunny") {
		t.Errorf("thinking emitted after content")
	}
}

func TestPerplexityWebAuthFailure(t *testing.T) {
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"session expired"}`))
	})
	rec, err := pplxDo(t, pplxReq(t, pplxTestBody("pplx-auto", "hi"), false))
	if err == nil {
		t.Fatal("401 accepted")
	}
	var upstream *proxy.UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", err)
	}
	if !strings.Contains(err.Error(), "session cookie may be expired") {
		t.Errorf("message = %v", err)
	}
	_ = rec
}

func TestPerplexityWebRateLimited(t *testing.T) {
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := pplxDo(t, pplxReq(t, pplxTestBody("pplx-auto", "hi"), false))
	var upstream *proxy.UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %v, want 429", err)
	}
}

func TestPerplexityWebToolsHint(t *testing.T) {
	tools := []any{
		map[string]any{"function": map[string]any{
			"name":        "run_shell",
			"description": "Run a bash command.\nSecond line should not appear.",
		}},
	}
	hint := pplxFormatToolsHint(tools)
	if !strings.Contains(hint, "- run_shell: Run a bash command.") {
		t.Errorf("hint = %q", hint)
	}
	if strings.Contains(hint, "Second line") {
		t.Errorf("description not truncated to first line: %q", hint)
	}
	if got := pplxFormatToolsHint(nil); got != "" {
		t.Errorf("no-tools hint = %q", got)
	}
}

func TestPerplexityWebFollowUpCarriesBackendUUID(t *testing.T) {
	var history []pplxHistoryItem
	current := "and then?"

	var captured map[string]any
	pplxTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		captured = sent
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(pplxSSEFrames(`{"status":"COMPLETED","text":"ok"}`)))
	})
	// The seed must land AFTER the setup: the setup resets the ledger.
	pplxSessionStoreFn(history, "first", "answer one", "bk-77")

	req := pplxReq(t, pplxTestBody("pplx-auto", ""), false)
	full := []any{
		map[string]any{"role": "user", "content": "first"},
		map[string]any{"role": "assistant", "content": "answer one"},
		map[string]any{"role": "user", "content": current},
	}
	reqBody, _ := json.Marshal(map[string]any{"model": "pplx-auto", "messages": full})
	req.Body = reqBody
	if _, err := pplxDo(t, req); err != nil {
		t.Fatalf("executor: %v", err)
	}
	params := captured["params"].(map[string]any)
	if params["last_backend_uuid"] != "bk-77" {
		t.Errorf("last_backend_uuid = %v", params["last_backend_uuid"])
	}
}

// pplxDo helpers keep the config type local; the executor reads the request
// fields directly and no Config override is needed for the URL-var seam.
type providersConfigType = struct{}

var _ = fmt.Sprintf
var _ = strings.Contains
