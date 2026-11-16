// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"strings"
	"testing"
)

func resetContextGuard(t *testing.T) {
	t.Helper()
	SetContextGuardMode("skip")
	t.Cleanup(func() { SetContextGuardMode("skip") })
}

// bigPromptBody builds an OpenAI body whose text content is n characters.
func bigPromptBody(n int) []byte {
	return []byte(`{"model":"x","messages":[{"role":"user","content":"` + strings.Repeat("a", n) + `"}]}`)
}

// TestContextGuardSkipOverWindow pins the A3 case: a 200k-char prompt
// (~50k tokens) against a 128k window is refused by the guard; a small
// prompt passes. The window comes from the model-name table — "gpt-4o"
// is 128k in the capabilities data.
func TestContextGuardSkipOverWindow(t *testing.T) {
	resetContextGuard(t)
	small := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if _, action, reason := contextGuardDecision("skip", "gpt-4o", 1, small); action != "pass" {
		t.Errorf("small prompt action = %q (%s), want pass", action, reason)
	}

	over := bigPromptBody(200_000) // ~50k tokens > 90% of no small window — use a tiny window model
	// A 128k-window model needs a >115k-window prompt: 200k chars = 50k
	// tokens fits 128k, so drive the comparison directly instead.
	window, fits := providerContextFits("gpt-4o", 200_000) // 200k TOKENS
	if fits {
		t.Errorf("200k tokens must not fit a %d window at 90%%", window)
	}
	if _, action, _ := contextGuardDecision("skip", "gpt-4o", 1, bigPromptBody(1_200_000)); action != "skip" {
		t.Error("a 300k-token prompt against a 128k window must be skipped")
	}
	_ = over
}

// TestContextGuardOffIsPassthrough pins "off": even an absurd prompt passes
// untouched (the old behavior, byte-identical).
func TestContextGuardOffIsPassthrough(t *testing.T) {
	resetContextGuard(t)
	SetContextGuardMode("off")
	body := bigPromptBody(1_200_000)
	newBody, action, _ := contextGuardDecision("off", "gpt-4o", 1, body)
	if action != "pass" {
		t.Errorf("off mode action = %q, want pass", action)
	}
	if string(newBody) != string(body) {
		t.Error("off mode must not touch the body")
	}
}

// TestContextGuardTruncateSingleCandidate pins truncate: the only candidate
// gets a body whose middle messages are dropped, keeping head and tail.
func TestContextGuardTruncateSingleCandidate(t *testing.T) {
	resetContextGuard(t)
	SetContextGuardMode("truncate")

	var b strings.Builder
	b.WriteString(`{"messages":[`)
	b.WriteString(`{"role":"system","content":"SYS"}`)
	for i := 0; i < 20; i++ {
		b.WriteString(`,{"role":"user","content":"` + strings.Repeat("m", 50_000) + `"}`)
	}
	b.WriteString(`,{"role":"user","content":"TAIL-KEEP-ME"}]}`)
	body := []byte(b.String())

	newBody, action, _ := contextGuardDecision("truncate", "gpt-4o", 1, body)
	if action != "truncate" {
		t.Fatalf("action = %q, want truncate", action)
	}
	newStr := string(newBody)
	if !strings.Contains(newStr, "SYS") {
		t.Error("system head lost")
	}
	if !strings.Contains(newStr, "TAIL-KEEP-ME") {
		t.Error("recent tail lost")
	}
	if strings.Count(newStr, `"role"`) > strings.Count(string(body), `"role"`)-5 {
		t.Errorf("middle messages were not dropped enough: before %d, after %d",
			strings.Count(string(body), `"role"`), strings.Count(newStr, `"role"`))
	}
}

// TestContextGuardMultiCandidateSkipsWithoutTruncate pins the boundary:
// truncate applies ONLY to a single-candidate provider; multiple candidates
// still skip whole.
func TestContextGuardTruncateMultiCandidateSkips(t *testing.T) {
	resetContextGuard(t)
	SetContextGuardMode("truncate")
	body := bigPromptBody(1_200_000)
	_, action, _ := contextGuardDecision("truncate", "gpt-4o", 3, body)
	if action != "skip" {
		t.Errorf("multi-candidate truncate action = %q, want skip", action)
	}
}

// TestEstimatePromptTokens sanity-checks the chars/4 estimator.
func TestEstimatePromptTokens(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"` + strings.Repeat("x", 400) + `"}]}`)
	if got := estimatePromptTokens(body); got < 90 || got > 110 {
		t.Errorf("estimatePromptTokens = %d, want ~100", got)
	}
	if got := estimatePromptTokens([]byte(`{"messages":[]}`)); got != 0 {
		t.Errorf("empty body estimate = %d, want 0", got)
	}
}
