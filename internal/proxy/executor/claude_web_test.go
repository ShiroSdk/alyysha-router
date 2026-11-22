// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package executor

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"alyysha/proxy/internal/proxy"
)

// claudeTestEnv aims the executor at a fake claude.ai endpoint through the
// plain doer (CLAUDE_WEB_PLAIN_HTTP=1), so headers, payload handling, and the
// stream adaptation are exercised without the browser-fingerprint TLS stack
// and without touching claude.ai.
func claudeTestEnv(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	t.Setenv("CLAUDE_WEB_PLAIN_HTTP", "1")
	site := httptest.NewServer(handler)
	t.Cleanup(site.Close)
	prevChat := claudeWebChatURL
	claudeWebChatURL = site.URL
	t.Cleanup(func() { claudeWebChatURL = prevChat })
	prevNew, prevConv := claudeWebNewConversationURL, claudeWebConversationURL
	claudeWebNewConversationURL = site.URL + "/orgs/%s/chat_conversations"
	claudeWebConversationURL = site.URL + "/orgs/%s/chat_conversations/%s"
	t.Cleanup(func() { claudeWebNewConversationURL, claudeWebConversationURL = prevNew, prevConv })

	prevEnabled := ClaudeWebEnabled()
	SetClaudeWebEnabled(true)
	t.Cleanup(func() { SetClaudeWebEnabled(prevEnabled) })

	claudeWebResetSessionState()
	t.Cleanup(claudeWebResetSessionState)
	return site
}

func claudeTestRequest(t *testing.T, body string, apiKey string) (*httptest.ResponseRecorder, *Request) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := &Request{
		Ctx:          t.Context(),
		Body:         []byte(body),
		APIKey:       apiKey,
		IsStream:     true,
		ConnectionID: "claude-test-" + apiKey[:min(8, len(apiKey))],
	}
	return rec, req
}

// TestClaudeWebDisabledByDefault pins the opt-in contract: the lane is OFF
// unless the env var or the dashboard toggle says otherwise, and a disabled
// request is refused before any network traffic.
func TestClaudeWebDisabledByDefault(t *testing.T) {
	t.Setenv("CLAUDE_WEB_PLAIN_HTTP", "1")
	prevChat := claudeWebChatURL
	claudeWebChatURL = "http://127.0.0.1:1/never-called"
	t.Cleanup(func() { claudeWebChatURL = prevChat })
	prevEnabled := ClaudeWebEnabled()
	SetClaudeWebEnabled(false)
	t.Cleanup(func() { SetClaudeWebEnabled(prevEnabled) })

	rec, req := claudeTestRequest(t, `{"model":"claude-web","messages":[{"role":"user","content":"hi"}],"stream":true}`, "sk-test")
	err := ForwardClaudeWeb(rec, req)
	if err == nil {
		t.Fatal("disabled lane accepted a request")
	}
	ue, ok := err.(*proxy.UpstreamError)
	if !ok {
		t.Fatalf("error = %T, want *proxy.UpstreamError", err)
	}
	if ue.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", ue.StatusCode)
	}
	if !strings.Contains(string(ue.Body), "claude_web_disabled") {
		t.Errorf("body = %s, want the claude_web_disabled code", ue.Body)
	}
}

// TestClaudeWebCookieHeader covers the cookie shapes the operator may paste.
func TestClaudeWebCookieHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare value", "sk-ant-sid01-abc", "sessionKey=sk-ant-sid01-abc"},
		{"full pair", "sessionKey=sk-ant-sid01-abc", "sessionKey=sk-ant-sid01-abc"},
		{"full header passthrough", "sessionKey=sk-ant-sid01-abc; intercom-id=xyz", "sessionKey=sk-ant-sid01-abc; intercom-id=xyz"},
		{"cookie label stripped", "Cookie: sessionKey=sk-ant-sid01-abc", "sessionKey=sk-ant-sid01-abc"},
		{"placeholder anonymous", "anonymous", ""},
		{"placeholder public", "public", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClaudeWebCookieHeader(tt.input); got != tt.want {
				t.Errorf("ClaudeWebCookieHeader(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
	if got := ClaudeWebSessionKey("sessionKey=sk-ant-sid01-abc; other=1"); got != "sk-ant-sid01-abc" {
		t.Errorf("ClaudeWebSessionKey = %q, want the sessionKey value", got)
	}
}

// TestClaudeIsCloudflareChallenge pins the interstitial markers.
func TestClaudeIsCloudflareChallenge(t *testing.T) {
	if !ClaudeIsCloudflareChallenge("Just a moment...") {
		t.Error("challenge marker missed")
	}
	if ClaudeIsCloudflareChallenge(`{"error":{"message":"rate limited"}}`) {
		t.Error("plain error read as a challenge")
	}
}

// TestForwardClaudeWebStream exercises the full turn against a fake upstream:
// request headers, the conversation reuse, the OpenAI chunk shape, and the
// local usage estimate.
func TestForwardClaudeWebStream(t *testing.T) {
	var gotCookie, gotUA string
	var turnCount int
	site := claudeTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotUA = r.Header.Get("User-Agent")
		turnCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"completion\":\"hello \"}\n\ndata: {\"completion\":\"from claude\"}\n\ndata: [DONE]\n\n"))
	})
	_ = site

	rec, req := claudeTestRequest(t, `{"model":"claude-web","messages":[{"role":"user","content":"hi there"}],"stream":true}`, "sk-ant-sid01-abc")
	req.ResponseBuf = &strings.Builder{}
	if err := ForwardClaudeWeb(rec, req); err != nil {
		t.Fatalf("forward: %v", err)
	}

	if gotCookie != "sessionKey=sk-ant-sid01-abc" {
		t.Errorf("upstream cookie = %q, want sessionKey=sk-ant-sid01-abc", gotCookie)
	}
	if gotUA != claudeWebUserAgent {
		t.Errorf("upstream UA = %q, want the consistent Chrome identity", gotUA)
	}
	if turnCount != 1 {
		t.Errorf("upstream turns = %d, want 1", turnCount)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"claude-web"`) {
		t.Errorf("chunks carry the model: %s", body)
	}
	if !strings.Contains(body, `"content":"hello "`) || !strings.Contains(body, `"content":"from claude"`) {
		t.Errorf("deltas missing: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) || !strings.Contains(body, "data: [DONE]") {
		t.Errorf("terminal chunk missing: %s", body)
	}
	if !strings.Contains(body, `"prompt_tokens"`) || !strings.Contains(body, `"completion_tokens"`) {
		t.Errorf("local usage estimate missing: %s", body)
	}
	// ResponseBuf captured the answer text for token accounting.
	if req.ResponseBuf.(*strings.Builder).String() != "hello from claude" {
		t.Errorf("response buffer = %q, want the full answer", req.ResponseBuf.(*strings.Builder).String())
	}
}

// TestForwardClaudeWebMissingCookie refuses the turn with the actionable
// message before any upstream call.
func TestForwardClaudeWebMissingCookie(t *testing.T) {
	claudeTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called without a cookie")
	})
	rec, req := claudeTestRequest(t, `{"messages":[{"role":"user","content":"hi"}],"stream":false}`, "anonymous")
	err := ForwardClaudeWeb(rec, req)
	if err == nil {
		t.Fatal("missing cookie accepted")
	}
	ue, ok := err.(*proxy.UpstreamError)
	if !ok || ue.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want 401 UpstreamError", err)
	}
	if !strings.Contains(string(ue.Body), "missing_session") {
		t.Errorf("body = %s, want missing_session", ue.Body)
	}
}

// TestForwardClaudeWebRefusals covers the failure classifiers.
func TestForwardClaudeWebRefusals(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantSub string
	}{
		{"cloudflare", http.StatusForbidden, "<html>Just a moment...</html>", "Cloudflare challenge"},
		{"expired", http.StatusUnauthorized, `{"error":"unauthenticated"}`, "sessionKey cookie may be expired"},
		{"rate limited", http.StatusTooManyRequests, `{"error":"too many"}`, "rate limited by claude.ai"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			rec, req := claudeTestRequest(t, `{"messages":[{"role":"user","content":"hi"}],"stream":false}`, "sk-ant-sid01-abc")
			err := ForwardClaudeWeb(rec, req)
			if err == nil {
				t.Fatalf("%s: turn accepted a refusal", tt.name)
			}
			ue, ok := err.(*proxy.UpstreamError)
			if !ok {
				t.Fatalf("%s: err = %T, want UpstreamError", tt.name, err)
			}
			if !strings.Contains(string(ue.Body), tt.wantSub) {
				t.Errorf("%s: body = %s, want it to mention %q", tt.name, ue.Body, tt.wantSub)
			}
		})
	}
}

// TestClaudeWebConversationReuse pins the lifecycle: one connection keeps its
// conversation for later turns, and the queue retires it after the turn.
func TestClaudeWebConversationReuse(t *testing.T) {
	var creations int
	site := claudeTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat_conversations") && r.Method == http.MethodPost {
			creations++
			_, _ = w.Write([]byte(`{"uuid":"conv-123"}`))
			return
		}
		_, _ = w.Write([]byte("data: {\"completion\":\"ok\"}\n\ndata: [DONE]\n\n"))
	})
	_ = site

	// With an organization id on the connection, the first turn creates the
	// conversation and the second reuses it.
	rec1 := httptest.NewRecorder()
	req1 := &Request{Ctx: t.Context(), Body: []byte(`{"messages":[{"role":"user","content":"one"}],"stream":false}`), APIKey: "sk-ant-sid01-abc", ConnectionID: "claude-reuse-test", ConnData: map[string]any{"organizationId": "org-1"}}
	if err := ForwardClaudeWeb(rec1, req1); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	rec2 := httptest.NewRecorder()
	req2 := &Request{Ctx: t.Context(), Body: []byte(`{"messages":[{"role":"user","content":"two"}],"stream":false}`), APIKey: "sk-ant-sid01-abc", ConnectionID: "claude-reuse-test", ConnData: map[string]any{"organizationId": "org-1"}}
	if err := ForwardClaudeWeb(rec2, req2); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if creations != 1 {
		t.Errorf("conversations created = %d, want 1 (second turn must reuse)", creations)
	}
	claudeWebResetSessionState()
}

// TestClaudeWebUsageEstimate checks the estimator bridge.
func TestClaudeWebUsageEstimate(t *testing.T) {
	usage := claudeWebUsage("hello", "world!")
	if usage == nil {
		t.Fatal("usage nil")
	}
	raw, _ := json.Marshal(usage)
	var parsed struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if parsed.PromptTokens == 0 || parsed.CompletionTokens == 0 || parsed.TotalTokens < parsed.PromptTokens+parsed.CompletionTokens {
		t.Errorf("usage = %s, want positive prompt/completion and a covering total", raw)
	}
}

// TestClaudeWebSessionKeyNeverLogged walks the claude_web sources and fails
// if the sessionKey value ever reaches a log or fmt call — B3's hard rule.
func TestClaudeWebSessionKeyNeverLogged(t *testing.T) {
	for _, name := range []string{
		"claude_web.go", "claude_web_cookie.go", "claude_web_transport.go",
		"claude_web_session.go", "claude_web_usage.go",
	} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "sessionkey") &&
				(strings.Contains(lower, "log.") || strings.Contains(lower, "fmt.print") || strings.Contains(lower, "fmt.sprintf")) {
				// fmt.Sprintf is allowed ONLY when the line does not interpolate the
				// cookie value — this gate is deliberately strict: any sessionKey
				// mention inside a log/print/format call fails.
				t.Errorf("%s:%d: sessionKey appears in a log/print/format call: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
