// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/log"
)

// Tool-loop watchdog (N2): an agent stuck producing the same tool call with
// the same arguments over and over burns tokens without progress. The
// watchdog tracks tool-call signatures per session across requests and
// intervenes when the same signature repeats past a threshold.
//
// chat.tool_loop_watch = off (default) | log | break
//	off   — no tracking, byte-identical to the old path.
//	log   — the repeated signature is logged and recorded on the explain
//	        panel; the response passes through.
//	break — the repeated response is withheld and the client gets a clean
//	        429 the agent can read, so the loop stops burning tokens.
//
// A break KEEPS the session record: the next response repeating the same
// signature breaks immediately, because an agent that retried without
// changing anything is still in the loop. Only a different signature — an
// agent that actually adjusted — resets the counter. Sessions age out on
// their own after the TTL, so the intervention never outlives the loop.

const (
	toolLoopDefaultThreshold = 3
	toolLoopSessionTTL       = 10 * time.Minute
	toolLoopMaxSessions      = 2048

	// ToolLoopBreakMessage is the client-facing refusal text. It names the
	// setting so an operator seeing it in an agent's transcript knows which
	// knob produced it.
	ToolLoopBreakMessage = "tool-loop guard: the same tool call has been returned too many times in a row for this session; the response was withheld to stop the loop (chat.tool_loop_watch=break)"
)

var (
	toolLoopWatchMode  atomic.Value // string: "off" | "log" | "break"
	toolLoopThreshold  atomic.Int64
	toolLoopSessionsMu sync.Mutex
	toolLoopSessions   map[string]*toolLoopSession // key: session ID
	toolLoopClock      = time.Now                  // test hook
)

type toolLoopSession struct {
	signature string // the last tool-call signature (empty = none yet)
	count     int    // how many consecutive times this signature appeared
	lastSeen  time.Time
}

func init() {
	toolLoopWatchMode.Store("off")
	toolLoopThreshold.Store(toolLoopDefaultThreshold)
	toolLoopSessions = make(map[string]*toolLoopSession)
}

// SetToolLoopWatch applies the stored setting. Unknown values read as off.
func SetToolLoopWatch(mode string) {
	switch mode {
	case "log", "break":
		toolLoopWatchMode.Store(mode)
	default:
		toolLoopWatchMode.Store("off")
	}
}

func toolLoopMode() string {
	if v, ok := toolLoopWatchMode.Load().(string); ok {
		return v
	}
	return "off"
}

// SetToolLoopThreshold applies the stored repeat count; values below 1 keep
// the previous one, since a threshold that fires on the first response would
// withhold every tool call.
func SetToolLoopThreshold(n int) {
	if n > 0 {
		toolLoopThreshold.Store(int64(n))
	}
}

func toolLoopThresholdValue() int {
	if n := toolLoopThreshold.Load(); n > 0 {
		return int(n)
	}
	return toolLoopDefaultThreshold
}

// toolLoopSignature hashes the tool name + arguments of every tool call in
// the response, producing one signature per response. Responses without tool
// calls carry no signature and never reach the watchdog.
func toolLoopSignature(toolCalls []map[string]any) string {
	if len(toolCalls) == 0 {
		return ""
	}
	var b strings.Builder
	for _, tc := range toolCalls {
		fn, _ := tc["function"].(map[string]any)
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)
		b.WriteString(name)
		b.WriteString("|")
		b.WriteString(args)
		b.WriteString("||")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// toolLoopCheck inspects the assembled tool-call signature for a session and
// returns the intervention action: "" (pass through), "log" (recorded, pass
// through) or "break" (withhold the response). Called once per response,
// after the full body is assembled, only when it carries tool calls.
func toolLoopCheck(sessionID string, signature string) (action string) {
	mode := toolLoopMode()
	if mode == "off" || signature == "" || sessionID == "" {
		return ""
	}
	now := toolLoopClock()

	toolLoopSessionsMu.Lock()
	defer toolLoopSessionsMu.Unlock()

	// Evict stale sessions before growing the map further; a sweep only runs
	// once the cap is under pressure, so the common path stays O(1).
	if len(toolLoopSessions) >= toolLoopMaxSessions {
		for k, s := range toolLoopSessions {
			if now.Sub(s.lastSeen) > toolLoopSessionTTL {
				delete(toolLoopSessions, k)
			}
		}
	}

	sess, ok := toolLoopSessions[sessionID]
	if !ok || now.Sub(sess.lastSeen) > toolLoopSessionTTL {
		// New session or stale entry: start fresh.
		toolLoopSessions[sessionID] = &toolLoopSession{signature: signature, count: 1, lastSeen: now}
		return ""
	}

	if sess.signature != signature {
		// A different call means the agent moved on; reset the counter.
		sess.signature = signature
		sess.count = 1
		sess.lastSeen = now
		return ""
	}

	// Same signature as the previous response.
	sess.count++
	sess.lastSeen = now
	if sess.count < toolLoopThresholdValue() {
		return ""
	}

	// Loop detected — and still detected on every further repeat, since the
	// counter only resets when the signature changes.
	reason := fmt.Sprintf("tool-loop: same tool-call signature repeated %d times (threshold %d)", sess.count, toolLoopThresholdValue())
	log.Warn("tool-loop", reason, "session", handlerutil.MaskAPIKey(sessionID), "signature", signature)
	RecordRoutingExplain(RoutingExplain{
		Provider: "tool_loop_watch",
		Reason:   reason,
		Outcome:  "skipped",
	})
	if mode == "break" {
		return "break"
	}
	return "log"
}

// toolLoopInspectBuffered scores a buffered chat.completion body against the
// watchdog and returns the intervention action. It parses the body only when
// the watchdog is armed, so the default path stays byte-for-byte the old one.
func toolLoopInspectBuffered(ctx context.Context, body []byte) string {
	if toolLoopMode() == "off" {
		return ""
	}
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []map[string]any `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Choices) == 0 {
		return ""
	}
	return toolLoopCheck(handlerutil.GetSessionID(ctx), toolLoopSignature(resp.Choices[0].Message.ToolCalls))
}

// toolLoopStreamAcc accumulates tool-call deltas from OpenAI-shaped SSE
// payloads while a streamed response flows through, so the watchdog can score
// the turn's final signature the moment the stream ends. It feeds from the
// same delta shape the buffered fold walks.
type toolLoopStreamAcc struct {
	armed       bool
	toolCallIdx map[int]*toolLoopStreamCall
}

type toolLoopStreamCall struct {
	name string
	args strings.Builder
}

// Feed walks one SSE payload. Frames that are not OpenAI chunks (keep-alives,
// [DONE], error envelopes) are ignored.
func (a *toolLoopStreamAcc) Feed(payload []byte) {
	if !a.armed {
		return
	}
	line := strings.TrimSpace(string(payload))
	line = strings.TrimPrefix(line, "data:")
	line = strings.TrimSpace(line)
	if line == "" || line == "[DONE]" {
		return
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(line), &chunk); err != nil || len(chunk.Choices) == 0 {
		return
	}
	for _, tc := range chunk.Choices[0].Delta.ToolCalls {
		if a.toolCallIdx == nil {
			a.toolCallIdx = map[int]*toolLoopStreamCall{}
		}
		entry, ok := a.toolCallIdx[tc.Index]
		if !ok {
			entry = &toolLoopStreamCall{}
			a.toolCallIdx[tc.Index] = entry
		}
		if tc.Function.Name != "" {
			if entry.name == "" {
				entry.name = tc.Function.Name
			} else {
				entry.name += tc.Function.Name
			}
		}
		entry.args.WriteString(tc.Function.Arguments)
	}
}

// Signature is the assembled tool-call signature for the stream, or "" when
// the turn carried no tool calls.
func (a *toolLoopStreamAcc) Signature() string {
	if len(a.toolCallIdx) == 0 {
		return ""
	}
	calls := make([]map[string]any, 0, len(a.toolCallIdx))
	for idx := 0; len(calls) < len(a.toolCallIdx); idx++ {
		entry, ok := a.toolCallIdx[idx]
		if !ok {
			continue
		}
		calls = append(calls, map[string]any{
			"function": map[string]any{"name": entry.name, "arguments": entry.args.String()},
		})
	}
	return toolLoopSignature(calls)
}

// toolLoopBreakJSON is the OpenAI-shaped error envelope a break writes into a
// stream or buffered reply, so an agent reading the transcript sees why the
// call was refused.
func toolLoopBreakJSON() []byte {
	b, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": ToolLoopBreakMessage,
			"type":    "tool_loop_break",
			"code":    "tool_loop",
		},
	})
	return b
}

// toolLoopBreakClaudeJSON is the same refusal in the Anthropic event shape a
// Claude-format client parses.
func toolLoopBreakClaudeJSON() []byte {
	b, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "tool_loop_break",
			"message": ToolLoopBreakMessage,
		},
	})
	return b
}

// writeToolLoopBreak answers the client with the loop refusal - the OpenAI
// envelope for Chat Completions callers, the Anthropic envelope for a
// Claude-format one. The request counts as handled: the intervention is the
// gateway's own decision, not an upstream failure the fallback ladder should
// retry, so the error travels straight to the agent instead of spending
// another provider's tokens on the same loop.
func writeToolLoopBreak(w http.ResponseWriter, translate bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	if translate {
		_, _ = w.Write(toolLoopBreakClaudeJSON())
		return
	}
	_, _ = w.Write(toolLoopBreakJSON())
}

// resetToolLoopWatch clears the session tracking; tests use it.
func resetToolLoopWatch() {
	toolLoopSessionsMu.Lock()
	toolLoopSessions = make(map[string]*toolLoopSession)
	toolLoopSessionsMu.Unlock()
}
