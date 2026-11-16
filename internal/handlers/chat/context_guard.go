// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	json "encoding/json/v2"
	"fmt"
	"sync/atomic"

	"9routerx/proxy/internal/providers"
	"9routerx/proxy/internal/translator"
)

// Context-aware rotation guard: a fallback candidate whose context window
// cannot hold the prompt is skipped instead of producing an upstream error
// mid-turn. The prompt size is ESTIMATED (characters/4 with a 10% margin via
// the 90% threshold) — no tokenizer, per the A3 constraint.
//
// fallback.context_guard = "skip" (default) | "truncate" | "off":
//
//	skip     — the provider is refused and the fallback chain moves on.
//	truncate — when the provider holds a single candidate, the middle of the
//	           message list is dropped (system head + recent tail kept) and
//	           the turn proceeds against it.
//	off      — the old behavior, byte-identical.
const (
	contextGuardCharsPerToken = 4
	// The prompt must fit inside 90% of the window: the estimate is coarse,
	// and the answer needs room too.
	contextGuardHeadroomPct   = 90
	contextGuardTruncateKeep  = 6 // recent messages kept from the tail
	contextGuardTruncateHeadN = 2 // non-system messages kept from the head
)

var contextGuardMode atomic.Value // string

func init() {
	contextGuardMode.Store("skip")
}

// SetContextGuardMode applies the stored setting; unknown values read as the
// default "skip".
func SetContextGuardMode(mode string) {
	switch mode {
	case "skip", "truncate", "off":
		contextGuardMode.Store(mode)
	default:
		contextGuardMode.Store("skip")
	}
}

func currentContextGuardMode() string {
	if v, ok := contextGuardMode.Load().(string); ok {
		return v
	}
	return "skip"
}

// estimatePromptTokens walks the OpenAI-shaped body and estimates the prompt
// size in tokens at four characters each. Zero for bodies without text.
func estimatePromptTokens(body []byte) int {
	var parsed struct {
		Messages []translator.OpenAIMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0
	}
	chars := 0
	for _, m := range parsed.Messages {
		chars += len(m.Role) + len(m.ReasoningContent)
		switch content := m.Content.(type) {
		case string:
			chars += len(content)
		case []any:
			for _, block := range content {
				if text, ok := block.(map[string]any); ok {
					if t, ok := text["text"].(string); ok {
						chars += len(t)
					}
				}
			}
		}
	}
	if chars == 0 {
		return 0
	}
	return chars / contextGuardCharsPerToken
}

// providerContextFits reports whether the estimated prompt fits inside 90%
// of the model's context window. Providers without a known window are always
// allowed (the guard refuses to act on guesses).
func providerContextFits(model string, estimatedTokens int) (window int, fits bool) {
	window, _ = providers.GetModelTokenLimits(model)
	if window <= 0 {
		return window, true
	}
	return window, estimatedTokens <= (window*contextGuardHeadroomPct)/100
}

// truncateMiddleBody is the real implementation: it rebuilds the request
// body with the truncated message list while preserving every other field.
func truncateMiddleBody(body []byte) []byte {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	messages, _ := raw["messages"].([]any)
	if len(messages) <= contextGuardTruncateHeadN+contextGuardTruncateKeep {
		return body
	}
	kept := make([]any, 0, contextGuardTruncateHeadN+contextGuardTruncateKeep)
	kept = append(kept, messages[:contextGuardTruncateHeadN]...)
	kept = append(kept, messages[len(messages)-contextGuardTruncateKeep:]...)
	raw["messages"] = kept
	out, err := json.Marshal(raw, json.Deterministic(true))
	if err != nil {
		return body
	}
	return out
}

// contextGuardDecision applies the mode for one provider candidate:
// pass (body unchanged), truncate (body replaced), or skip (provider
// refused with the reason). A zero window always passes.
func contextGuardDecision(mode, model string, allConns int, body []byte) (newBody []byte, action, reason string) {
	newBody = body
	if mode == "off" || allConns <= 0 {
		return body, "pass", ""
	}
	est := estimatePromptTokens(body)
	if est <= 0 {
		return body, "pass", ""
	}
	window, fits := providerContextFits(model, est)
	if fits || window <= 0 {
		return body, "pass", ""
	}
	limit := (window * contextGuardHeadroomPct) / 100
	reason = fmt.Sprintf("estimated prompt %d tokens exceeds %d-token window for %s", est, limit, model)
	if mode == "truncate" && allConns == 1 {
		return truncateMiddleBody(body), "truncate", reason + "; truncated for the only candidate"
	}
	return body, "skip", reason
}
