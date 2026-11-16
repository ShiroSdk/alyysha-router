// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package executor

import (
	"strings"
)

// The claude.ai credential is the sessionKey browser cookie. The dashboard
// probe shares these helpers so the executor and the "Check" button read the
// credential exactly the same way (the gemini-web precedent). The value is
// never logged anywhere — it only flows into the Cookie header.

const claudeWebCookieName = "sessionKey"

// ClaudeWebUserAgent is the browser identity every claude-web request sends.
// It must stay consistent with the TLS profile in claude_web_transport.go.
func ClaudeWebUserAgent() string {
	return claudeWebUserAgent
}

// ClaudeWebCookieHeader turns whatever the operator pasted into a Cookie
// header: a full header passes through unchanged, a bare sessionKey value
// becomes "sessionKey=<v>", and placeholder keys produce nothing.
func ClaudeWebCookieHeader(apiKey string) string {
	raw := strings.TrimSpace(apiKey)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if lower == "anonymous" || lower == "public" {
		return ""
	}
	if strings.Contains(raw, "=") {
		// A full header or a name=value pair: pass through, stripped of labels.
		raw = strings.TrimPrefix(raw, "Cookie:")
		return strings.TrimSpace(raw)
	}
	return claudeWebCookieName + "=" + raw
}

// ClaudeWebSessionKey extracts the sessionKey value from a pasted header or
// bare value. Empty when the input carries no sessionKey at all.
func ClaudeWebSessionKey(apiKey string) string {
	header := ClaudeWebCookieHeader(apiKey)
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), claudeWebCookieName) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
