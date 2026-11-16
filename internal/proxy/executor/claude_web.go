// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	json "encoding/json/v2"

	"9routerx/proxy/internal/handlerutil"
	"9routerx/proxy/internal/log"
	"9routerx/proxy/internal/proxy"
	"9routerx/proxy/internal/translator"
	"9routerx/proxy/internal/webexec"
)

// claude-web talks to claude.ai's web conversation lane with the sessionKey
// cookie a logged-in browser carries. It is an UNOFFICIAL provider: it
// violates Anthropic's terms of service, the endpoints below are unverified
// (no live capture was possible during development — see TODO_OWNER), and
// claude.ai can change or shut them at any time. It ships OFF and only runs
// when CLAUDE_WEB_ENABLED=true or the operator flips the dashboard toggle;
// the env var is the operator's kill-switch and overrides the stored setting.

const (
	// TODO_OWNER: the append_message endpoint and its payload shape could not
	// be verified against a real logged-in browser during development. Verify
	// both against DevTools before trusting the live path.
	claudeWebTimeout     = 120 * time.Second
	claudeWebRatePerConn = 20 // requests per minute per connection, webexec window
)

// The lane endpoints are vars so tests can aim them at an httptest fake.
var claudeWebChatURL = "https://claude.ai/api/append_message"

// Per-connection rate limit so a looping agent cannot hammer claude.ai: the
// webexec sliding-window limiter keyed by connection id.
var claudeWebRate = webexec.NewRateLimiter(claudeWebRatePerConn, time.Minute)

// ForwardClaudeWeb routes a request to the claude.ai web lane.
func ForwardClaudeWeb(w http.ResponseWriter, req *Request) error {
	if !CheckClaudeWebAllowed(w, "claude-web") {
		return &proxy.UpstreamError{
			StatusCode: http.StatusForbidden,
			Body:       []byte(`{"error":{"code":"claude_web_disabled","message":"claude-web is disabled by policy","type":"policy_error"}}`),
		}
	}

	connectionID := connectionIDForClaudeWeb(req)
	if !claudeWebRate.Allow(connectionID, time.Now()) {
		return proxy.UpstreamFailure(http.StatusTooManyRequests,
			"claude-web rate limited: more than "+fmt.Sprint(claudeWebRatePerConn)+" requests per minute on one connection. Wait a moment and retry.")
	}

	var parsed struct {
		Model    string                     `json:"model"`
		Messages []translator.OpenAIMessage `json:"messages"`
		Stream   bool                       `json:"stream"`
	}
	if err := json.Unmarshal(req.Body, &parsed); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "claude-web: invalid request body")
		return err
	}

	cookie := resolveClaudeWebCookie(req)
	if cookie == "" {
		handlerutil.WriteJSONErrorCode(w, http.StatusUnauthorized, "missing_session",
			"claude-web: missing sessionKey cookie — paste the sessionKey value from a logged-in claude.ai browser")
		return &proxy.UpstreamError{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(`{"error":{"code":"missing_session","message":"claude-web: missing sessionKey cookie","type":"auth_error"}}`),
		}
	}

	ctx := req.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, claudeWebTimeout)
	defer cancel()

	prompt := renderClaudeWebMessages(parsed.Messages)
	if strings.TrimSpace(prompt) == "" {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "claude-web: no text content in messages")
		return fmt.Errorf("claude-web: no text content in messages")
	}

	// Conversation lifecycle: create on the first turn of a connection, reuse
	// for the rest, delete when the turn completes.
	conversationID, err := ensureClaudeWebConversation(ctx, req, cookie, connectionID)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadGateway, "claude-web: conversation setup failed: "+err.Error())
		return err
	}

	payload := claudeWebBuildPayload(prompt, conversationID)
	payloadBytes, err := json.Marshal(payload, json.Deterministic(true))
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "claude-web: marshal error")
		return err
	}

	resp, err := claudeWebDo(ctx, http.MethodPost, claudeWebChatURL, map[string]string{
		"Content-Type": "application/json",
		"Accept":       "text/event-stream",
		"Cookie":       cookie,
		"Origin":       "https://claude.ai",
		"Referer":      "https://claude.ai/chat",
	}, payloadBytes)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadGateway, "claude-web: upstream request failed: "+err.Error())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		err := claudeWebStatusFailure(resp.StatusCode, string(bodyBytes))
		if se, ok := err.(*proxy.UpstreamError); ok {
			handlerutil.WriteJSONError(w, se.StatusCode, fmt.Sprintf("claude-web: upstream error %d: %s", se.StatusCode, string(se.Body)))
		} else {
			handlerutil.WriteJSONError(w, http.StatusBadGateway, err.Error())
		}
		return err
	}

	out := claudeWebStreamResponse(w, resp.Body, parsed.Model, req.IsStream, req, prompt)
	// Housekeeping mirrors deepseek-web: the conversation is queued for
	// deletion once the turn is done, never logged, never reused after close.
	claudeWebQueueConversationDelete(req, cookie, conversationID)
	log.Info("claude-web", "turn complete", "connection", connectionID, "conversation", conversationID)
	return out
}

// claudeWebBuildPayload assembles the append_message body.
//
// TODO_OWNER: the field names below follow the endpoint's documented shape at
// the time of writing but were never verified against a live session. If the
// live path fails with 400s, capture the real browser request in DevTools and
// adjust here.
func claudeWebBuildPayload(prompt, conversationID string) map[string]any {
	return map[string]any{
		"prompt":      prompt,
		"timezone":    "Asia/Jakarta",
		"attachments": []any{},
		"files":       []any{},
		"rendering_mode": map[string]any{
			"renderer": "markdown",
		},
		"model":           nil, // claude.ai picks its own web model; no upstream model id is invented here
		"conversation_id": conversationID,
	}
}

func connectionIDForClaudeWeb(req *Request) string {
	if req.ConnectionID != "" {
		return req.ConnectionID
	}
	return req.APIKey
}

func resolveClaudeWebCookie(req *Request) string {
	header := ClaudeWebCookieHeader(req.APIKey)
	if header != "" {
		return header
	}
	if req.ConnData != nil {
		for _, key := range []string{"sessionKey", "apiKey", "cookie"} {
			if v, ok := req.ConnData[key].(string); ok && strings.TrimSpace(v) != "" {
				if h := ClaudeWebCookieHeader(v); h != "" {
					return h
				}
			}
		}
	}
	return ""
}

func renderClaudeWebMessages(messages []translator.OpenAIMessage) string {
	var parts []string
	for _, m := range messages {
		text := ""
		if s, ok := m.Content.(string); ok {
			text = s
		}
		if text != "" {
			parts = append(parts, fmt.Sprintf("[%s]: %s", m.Role, text))
		}
	}
	return strings.Join(parts, "\n\n")
}

// claudeWebStreamResponse adapts whatever claude.ai streams back into OpenAI
// chunks. The lane's event shape is unverified (TODO_OWNER above), so the
// parser is deliberately defensive: any known text carrier is accepted, and
// usage is always estimated locally.
func claudeWebStreamResponse(w http.ResponseWriter, upstream io.Reader, model string, isStream bool, req *Request, prompt string) error {
	scanner := bufio.NewScanner(upstream)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	flusher, isFlusher := w.(http.Flusher)
	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if isFlusher {
			flusher.Flush()
		}
	}

	var accumulated strings.Builder
	id := fmt.Sprintf("chatcmpl-claudeweb-%d", time.Now().UnixMilli())

	emit := func(delta string) {
		accumulated.WriteString(delta)
		if req.ResponseBuf != nil {
			_, _ = io.WriteString(req.ResponseBuf, delta)
		}
		if !isStream {
			return
		}
		chunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]string{"content": delta},
					"finish_reason": nil,
				},
			},
		}
		chunkBytes, _ := json.Marshal(chunk, json.Deterministic(true))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
		if isFlusher {
			flusher.Flush()
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			break
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if delta := claudeWebEventText(event); delta != "" {
			emit(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("claude-web: stream read: %w", err)
	}

	usage := claudeWebUsage(prompt, accumulated.String())
	if isStream {
		termChunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": "stop",
				},
			},
			"usage": usage,
		}
		termBytes, _ := json.Marshal(termChunk, json.Deterministic(true))
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", string(termBytes))
		if isFlusher {
			flusher.Flush()
		}
		return nil
	}

	fullResp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": accumulated.String(),
				},
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	}
	handlerutil.WriteJSON(w, http.StatusOK, fullResp)
	return nil
}

// claudeWebEventText pulls assistant text out of one streamed event. Every
// carrier claude.ai's web lane has been seen or documented to use is checked
// in order; anything unknown is skipped rather than guessed.
func claudeWebEventText(event map[string]any) string {
	if c, ok := event["completion"].(string); ok && c != "" {
		return c
	}
	if delta, ok := event["delta"].(map[string]any); ok {
		if t, ok := delta["text"].(string); ok && t != "" {
			return t
		}
	}
	if c, ok := event["content"].(string); ok && c != "" {
		return c
	}
	return ""
}

func claudeWebStatusFailure(status int, body string) error {
	switch {
	case ClaudeIsCloudflareChallenge(body):
		return proxy.UpstreamFailure(status,
			"claude-web returned a Cloudflare challenge — the sessionKey cookie was copied from a different browser or machine. Re-copy the sessionKey from a logged-in claude.ai browser on this machine.")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return proxy.UpstreamFailure(status,
			"claude-web auth failed — the sessionKey cookie may be expired or the session revoked. Re-copy it from claude.ai.")
	case status == http.StatusTooManyRequests:
		return proxy.UpstreamFailure(status,
			"claude-web rate limited by claude.ai. Wait a moment and retry; heavy use of the free web tier risks a temporary block.")
	}
	first := strings.TrimSpace(body)
	if len(first) > 200 {
		first = first[:200]
	}
	return proxy.UpstreamFailure(status, fmt.Sprintf("claude-web returned HTTP %d %s", status, first))
}
