// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package executor

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/log"
	"alyysha/proxy/internal/proxy"
)

// perplexity.ai's web chat (schematized SSE API), ported from the upstream
// executor (open-sse/executors/perplexity-web.ts + perplexity-web/protocol.ts).
// One POST to the SSE endpoint with the __Secure-next-auth.session-token
// cookie answers a JSON event stream: pro-search steps, plan goals, markdown
// diffs for the answer, a backend uuid for continuation, and a COMPLETED
// terminal. The cookie executes through the same session machinery the
// dashboard's browser-login recipe captures.

const (
	perplexityWebOrigin  = "https://www.perplexity.ai"
	perplexityWebSSEPath = "/rest/sse/perplexity_ask"
	pplxAPIVersion       = "2.18"
	pplxStreamEOF        = "event: end_of_stream"
	pplxQueryBudget      = 96000
	pplxSessionTTL       = time.Hour
	pplxSessionMax       = 200
	pplxRequestTimeout   = 5 * time.Minute
)

// perplexityWebChatURL is the endpoint every turn posts to; var so the test
// suite can aim it at a fake site.
var perplexityWebChatURL = perplexityWebOrigin + perplexityWebSSEPath

// pplxModelRoute is the wire translation for one client-facing model name:
// the mode the ask endpoint expects plus the model_preference it routes to.
// Every entry runs mode "copilot" — the backend downgrades mode "search" to
// CONCISE, drops model_preference, and answers {"status":"FAILED"}.
type pplxModelRoute struct {
	mode       string
	modelPref  string
	isThinking bool
}

var pplxModelMap = map[string]pplxModelRoute{
	"pplx-auto":     {mode: "copilot", modelPref: "pplx_pro"},
	"pplx-sonar":    {mode: "copilot", modelPref: "turbo"},
	"pplx-gpt":      {mode: "copilot", modelPref: "gpt56_terra"},
	"pplx-gemini":   {mode: "copilot", modelPref: "gemini37flash"},
	"pplx-sonnet":   {mode: "copilot", modelPref: "claude50sonnet"},
	"pplx-opus":     {mode: "copilot", modelPref: "claude50opus"},
	"pplx-nemotron": {mode: "copilot", modelPref: "nv_nemotron_3_ultra"},
}

// pplxThinkingMap is the model_preference swap a thinking request gets. A
// thinking request always runs mode "copilot" too.
var pplxThinkingMap = map[string]string{
	"pplx-gpt":      "gpt56_terra_thinking",
	"pplx-gemini":   "gemini37flashthinking",
	"pplx-sonnet":   "claude50sonnetthinking",
	"pplx-opus":     "claude50opusthinking",
	"pplx-nemotron": "nv_nemotron_3_ultra_thinking",
}

func pplxResolveModel(model string) pplxModelRoute {
	route, ok := pplxModelMap[model]
	if !ok {
		log.Info("perplexity-web", "unmapped model, defaulting to pplx-auto", "model", model)
		route = pplxModelMap["pplx-auto"]
	}
	if variant, thinking := pplxThinkingMap[model]; thinking {
		route.isThinking = true
		route.modelPref = variant
	}
	return route
}

// ——— request parsing (upstream parseOpenAIMessages) ———

type pplxHistoryItem struct {
	role    string
	content string
}

type pplxParsedMessages struct {
	system  string
	history []pplxHistoryItem
	current string
}

// pplxParseOpenAIMessages folds the OpenAI messages array into the ask
// endpoint's shape: system content accumulates into one contract string,
// user/assistant turns become history in order, and only a trailing user
// message becomes the current query.
func pplxParseOpenAIMessages(messages []any) pplxParsedMessages {
	parsed := pplxParsedMessages{}
	for _, mAny := range messages {
		msg, ok := mAny.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "developer" {
			role = "system"
		}
		var parts []string
		switch content := msg["content"].(type) {
		case string:
			parts = append(parts, content)
		case []any:
			for _, blockAny := range content {
				block, ok := blockAny.(map[string]any)
				if !ok {
					continue
				}
				if blockType, _ := block["type"].(string); blockType == "text" {
					text, _ := block["text"].(string)
					parts = append(parts, text)
				}
			}
		}
		contentStr := strings.Join(parts, " ")
		if strings.TrimSpace(contentStr) == "" {
			continue
		}
		switch role {
		case "system":
			parsed.system += contentStr + "\n"
		case "user", "assistant":
			parsed.history = append(parsed.history, pplxHistoryItem{role: role, content: contentStr})
		}
	}
	if len(parsed.history) > 0 && parsed.history[len(parsed.history)-1].role == "user" {
		parsed.current = parsed.history[len(parsed.history)-1].content
		parsed.history = parsed.history[:len(parsed.history)-1]
	}
	return parsed
}

// ——— tools hint (task contract: reference-only manifest in the query) ———

// pplxFormatToolsHint renders the client's OpenAI tools array as a
// reference-only manifest the model can name but is not expected to call
// back through. Empty when the request carries no usable tools.
func pplxFormatToolsHint(tools any) string {
	list, ok := tools.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	var entries []string
	for _, tAny := range list {
		tool, ok := tAny.(map[string]any)
		if !ok {
			continue
		}
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		if name == "" {
			name, _ = tool["name"].(string)
		}
		if name == "" {
			continue
		}
		desc, _ := function["description"].(string)
		if desc == "" {
			desc, _ = tool["description"].(string)
		}
		if idx := strings.IndexAny(desc, "\n\r"); idx >= 0 {
			desc = desc[:idx]
		}
		if len(desc) > 200 {
			desc = desc[:200]
		}
		if desc != "" {
			entries = append(entries, "- "+name+": "+desc)
		} else {
			entries = append(entries, "- "+name)
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return "\n\nAvailable tools (reference only — answer questions directly, do not attempt to call them):\n" + strings.Join(entries, "\n")
}

// ——— query assembly (upstream buildQuery) ———

// pplxBuildQuery assembles the turn's query_str. A follow-up carries the
// system contract above the current message; a first turn packs the whole
// contract object (instructions/history/query) as JSON and trims from the
// FRONT, keeping the LAST pplxQueryBudget characters — the newest context
// is what the endpoint must not lose.
func pplxBuildQuery(parsed pplxParsedMessages, toolsHint string, followUpUUID string) string {
	hint := toolsHint
	if parsed.system != "" {
		if hint != "" {
			hint = "\n" + strings.TrimLeft(hint, "\n")
		}
		contract := strings.TrimRight(parsed.system, "\n") + hint
		if followUpUUID != "" {
			if contract != "" {
				return contract + "\n\n" + parsed.current
			}
			return parsed.current
		}
		obj := map[string]any{
			"instructions": []string{strings.TrimSpace(parsed.system)},
		}
		if hint != "" {
			obj["instructions"] = []string{strings.TrimSpace(parsed.system) + hint}
		}
		if len(parsed.history) > 0 {
			history := make([]map[string]string, 0, len(parsed.history))
			for _, h := range parsed.history {
				history = append(history, map[string]string{"role": h.role, "content": h.content})
			}
			obj["history"] = history
		}
		if parsed.current != "" {
			obj["query"] = parsed.current
		} else if len(parsed.history) == 0 {
			obj["query"] = ""
		}
		encoded, err := json.Marshal(obj)
		if err != nil {
			return parsed.current
		}
		if len(encoded) > pplxQueryBudget {
			return string(encoded[len(encoded)-pplxQueryBudget:])
		}
		return string(encoded)
	}
	if hint != "" {
		hint = strings.TrimLeft(hint, "\n") + "\n\n"
	}
	return hint + parsed.current
}

// ——— session continuation (upstream session cache) ———

type pplxSessionEntry struct {
	backendUUID string
	ts          time.Time
}

var (
	pplxSessionMu     sync.Mutex
	pplxSessionCache  = map[string]pplxSessionEntry{}
	pplxSessionClock  = time.Now
	pplxSessionOrder  []string
	pplxSessionLookup = pplxSessionLookupFn
	pplxSessionStore  = pplxSessionStoreFn
)

// pplxSessionKey is FNV-1a 32-bit over "role:content" lines, hex 8 — the
// same derivation the upstream uses for its session cache.
func pplxSessionKey(history []pplxHistoryItem) string {
	var parts strings.Builder
	for _, h := range history {
		parts.WriteString(h.role)
		parts.WriteString(":")
		parts.WriteString(h.content)
		parts.WriteString("\n")
	}
	hash := uint64(0x811c9dc5)
	for _, b := range []byte(parts.String()) {
		hash ^= uint64(b)
		hash *= 0x01000193
		hash &^= 0 >> 63 // keep 32-bit semantics via mask below
		hash &= 0xFFFFFFFF
	}
	return fmt.Sprintf("%08x", hash)
}

// pplxSessionLookupFn returns the stored backend uuid for this history when
// one is fresh enough to continue the conversation.
func pplxSessionLookupFn(history []pplxHistoryItem) string {
	if len(history) == 0 {
		return ""
	}
	key := pplxSessionKey(history)
	now := pplxSessionClock()
	pplxSessionMu.Lock()
	defer pplxSessionMu.Unlock()
	entry, ok := pplxSessionCache[key]
	if !ok {
		return ""
	}
	if now.Sub(entry.ts) > pplxSessionTTL {
		delete(pplxSessionCache, key)
		return ""
	}
	return entry.backendUUID
}

// pplxSessionStoreFn records the backend uuid a completed turn produced,
// keyed by the history that turn extended (history + current + answer), so
// the next turn with the same prefix continues the same backend session.
func pplxSessionStoreFn(history []pplxHistoryItem, current, answer, backendUUID string) {
	if backendUUID == "" {
		return
	}
	extended := make([]pplxHistoryItem, 0, len(history)+2)
	extended = append(extended, history...)
	extended = append(extended, pplxHistoryItem{role: "user", content: current})
	extended = append(extended, pplxHistoryItem{role: "assistant", content: answer})
	key := pplxSessionKey(extended)
	now := pplxSessionClock()
	pplxSessionMu.Lock()
	defer pplxSessionMu.Unlock()
	if _, exists := pplxSessionCache[key]; !exists {
		pplxSessionOrder = append(pplxSessionOrder, key)
	}
	pplxSessionCache[key] = pplxSessionEntry{backendUUID: backendUUID, ts: now}
	for len(pplxSessionCache) > pplxSessionMax && len(pplxSessionOrder) > 0 {
		oldest := pplxSessionOrder[0]
		pplxSessionOrder = pplxSessionOrder[1:]
		delete(pplxSessionCache, oldest)
	}
}

// pplxSessionReset clears the session cache; tests use it.
func pplxSessionReset() {
	pplxSessionMu.Lock()
	pplxSessionCache = map[string]pplxSessionEntry{}
	pplxSessionOrder = nil
	pplxSessionMu.Unlock()
}

// ——— request body (upstream buildPplxRequestBody) ———

func pplxUUID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func pplxTimezone() string {
	return "UTC"
}

// pplxSupportedBlockUseCases is the use-case list the schematized API
// expects on every request (upstream PPLX_SUPPORTED_BLOCK_USE_CASES).
var pplxSupportedBlockUseCases = []string{
	"answer_modes", "media_items", "knowledge_cards", "step_points", "plan",
	"media_gen_params", "ads", "related", "steps", "pro_search_steps",
	"workflow_block", "diff_block", "answer", "costar", "clarify",
	"search_focus", "sources", "file_attachments", "favorites", "academic_focus",
	"social_search", "language", "query_breakdown", "ui_state", "generate_images",
	"deep_research", "comet", "browser_agent", "background_agents",
}

// pplxBuildBody builds the ask endpoint's body. dslQuery mirrors the
// structured form of the query; followUpUUID continues an earlier backend
// session when present.
func pplxBuildBody(query, dslQuery, mode, modelPref, followUpUUID, requestID string) map[string]any {
	params := map[string]any{
		"attachments":                          []any{},
		"language":                             "en-US",
		"timezone":                             pplxTimezone(),
		"search_focus":                         "internet",
		"sources":                              []string{"web"},
		"frontend_uuid":                        requestID,
		"mode":                                 mode,
		"model_preference":                     modelPref,
		"is_related_query":                     false,
		"is_sponsored":                         false,
		"frontend_context_uuid":                pplxUUID(),
		"prompt_source":                        "user",
		"query_source":                         "home",
		"is_incognito":                         true,
		"local_search_enabled":                 false,
		"use_schematized_api":                  true,
		"send_back_text_in_streaming_api":      false,
		"supported_block_use_cases":            pplxSupportedBlockUseCases,
		"client_coordinates":                   nil,
		"mentions":                             []any{},
		"dsl_query":                            dslQuery,
		"skip_search_enabled":                  true,
		"is_nav_suggestions_disabled":          false,
		"source":                               "default",
		"always_search_override":               false,
		"override_no_search":                   false,
		"client_search_results_cache_key":      requestID,
		"should_ask_for_mcp_tool_confirmation": true,
		"supports_tool_approval_modal":         true,
		"browser_agent_allow_once_from_toggle": false,
		"force_enable_browser_agent":           false,
		"supported_features":                   []string{"browser_agent_permission_banner_v1.1"},
		"extended_context":                     false,
		"version":                              pplxAPIVersion,
		"rum_session_id":                       pplxUUID(),
	}
	if dslQuery == "" {
		params["dsl_query"] = query
	}
	if followUpUUID != "" {
		params["last_backend_uuid"] = followUpUUID
	}
	return map[string]any{
		"query_str": query,
		"params":    params,
	}
}

// ——— response cleanup (upstream cleanResponse) ———

var (
	pplxCitationRe   = regexp.MustCompile(`\[\d+\]`)
	pplxGrokTagRe    = regexp.MustCompile(`(?s)<grok[^>]*>.*?</grok>`)
	pplxXMLDeclRe    = regexp.MustCompile(`(?s)<\?xml[^>]*\?>`)
	pplxResponseRe   = regexp.MustCompile(`(?s)</?response>`)
	pplxBlankRunsRe  = regexp.MustCompile(`\n{3,}`)
	pplxTrailSpaceRe = regexp.MustCompile(`[ \t]+\n`)
	pplxMultiSpaceRe = regexp.MustCompile(" {2,}")
)

// pplxCleanResponse removes the artifacts the endpoint sprinkles into the
// answer: numbered citations, foreign tags, and — when strip is set (the
// non-streaming path and the session ledger) — the blank-line runs. The
// streaming path keeps whitespace so code indentation survives.
func pplxCleanResponse(text string, strip bool) string {
	out := pplxCitationRe.ReplaceAllString(text, "")
	out = pplxGrokTagRe.ReplaceAllString(out, "")
	out = pplxXMLDeclRe.ReplaceAllString(out, "")
	out = pplxResponseRe.ReplaceAllString(out, "")
	if strip {
		out = pplxMultiSpaceRe.ReplaceAllString(out, " ")
		out = pplxTrailSpaceRe.ReplaceAllString(out, "\n")
		out = pplxBlankRunsRe.ReplaceAllString(out, "\n\n")
		out = strings.TrimSpace(out)
	}
	return out
}

// ——— SSE event stream parsing (upstream readPplxSseEvents) ———

// pplxScanSSE reads the endpoint's event stream and hands each decoded JSON
// event to fn. Multi-line data: frames are joined with "\n"; the stream ends
// on `event: end_of_stream`, a [DONE] sentinel, or EOF. Events that are not
// JSON are skipped.
func pplxScanSSE(upstream io.Reader, fn func(event map[string]any) bool) error {
	scanner := bufio.NewScanner(upstream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var dataLines []string
	flush := func() bool {
		if len(dataLines) == 0 {
			return true
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = nil
		if payload == "[DONE]" {
			return false
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return true
		}
		return fn(event)
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == pplxStreamEOF {
			_ = flush()
			return nil
		}
		if trimmed == "" {
			if !flush() {
				return nil
			}
			continue
		}
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimLeft(data, " "))
			continue
		}
		// event:/id:/retry: lines and site comments carry no payload here.
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	_ = flush()
	return nil
}

// ——— content extraction (upstream extractContent) ———

type pplxExtractState struct {
	chunks        []string
	answer        string
	emitted       int
	thinkingParts []string
	seenThinking  map[string]bool
	backendUUID   string
	lastText      string
	fullAnswer    string
}

// pplxDelta assembles the content delta the streaming client sees: the
// answer grew, emit the tail it has not received yet.
func (s *pplxExtractState) currentAnswer() string {
	longest := s.answer
	for _, c := range s.chunks {
		if len(c) > len(longest) {
			longest = c
		}
	}
	return longest
}

func (s *pplxExtractState) delta() string {
	current := s.currentAnswer()
	if len(current) <= s.emitted {
		return ""
	}
	delta := current[s.emitted:]
	s.emitted = len(current)
	return delta
}

// pplxExtractThinking pulls the pro-search narration out of one event:
// SEARCH_WEB steps become "Searching: query", READ_RESULTS the first three
// "Reading: url", deduplicated across the stream.
func (s *pplxExtractState) extractThinking(event map[string]any) string {
	usage, _ := event["usage"].(string)
	if usage != "pro_search_steps" {
		return ""
	}
	block, _ := event["block"].(map[string]any)
	planBlock, _ := block["plan_block"].(map[string]any)
	steps, _ := planBlock["steps"].([]any)
	if s.seenThinking == nil {
		s.seenThinking = map[string]bool{}
	}
	var out []string
	for _, stepAny := range steps {
		step, ok := stepAny.(map[string]any)
		if !ok {
			continue
		}
		stepType, _ := step["step_type"].(string)
		switch stepType {
		case "SEARCH_WEB":
			qr, _ := step["query"].(string)
			if qr == "" || s.seenThinking["q:"+qr] {
				continue
			}
			s.seenThinking["q:"+qr] = true
			out = append(out, "Searching: "+qr)
		case "READ_RESULTS":
			urls, _ := step["urls"].([]any)
			read := 0
			for _, uAny := range urls {
				if read >= 3 {
					break
				}
				u, _ := uAny.(string)
				if u == "" || s.seenThinking["u:"+u] {
					continue
				}
				s.seenThinking["u:"+u] = true
				out = append(out, "Reading: "+u)
				read++
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	joined := strings.Join(out, "\n")
	s.thinkingParts = append(s.thinkingParts, joined)
	return joined
}

// pplxExtractPlanGoals reads the plan block's goal descriptions — the
// roadmap narration a Pro search emits before it starts working.
func (s *pplxExtractState) extractPlanGoals(event map[string]any) string {
	usage, _ := event["usage"].(string)
	if usage != "plan" {
		return ""
	}
	block, _ := event["block"].(map[string]any)
	planBlock, _ := block["plan_block"].(map[string]any)
	goals, _ := planBlock["goals"].([]any)
	var out []string
	for _, gAny := range goals {
		g, ok := gAny.(map[string]any)
		if !ok {
			continue
		}
		desc, _ := g["description"].(string)
		if desc != "" {
			out = append(out, desc)
		}
	}
	// RFC-6902 diff patches carrying goals arrays are handled the same way.
	if diffs, ok := event["diffs"].([]any); ok {
		for _, dAny := range diffs {
			d, ok := dAny.(map[string]any)
			if !ok {
				continue
			}
			value, _ := d["value"].([]any)
			for _, gAny := range value {
				g, ok := gAny.(map[string]any)
				if !ok {
					continue
				}
				desc, _ := g["description"].(string)
				if desc != "" {
					out = append(out, desc)
				}
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	joined := strings.Join(out, "\n")
	s.thinkingParts = append(s.thinkingParts, joined)
	return joined
}

// pplxApplyMarkdownDiff folds one RFC-6902-style patch into the answer
// state: path "" replaces the whole answer (seeding chunks when the value
// is an array), path "/chunks/<n>" sets one chunk index.
func (s *pplxExtractState) applyMarkdownDiff(patches []any) {
	for _, pAny := range patches {
		patch, ok := pAny.(map[string]any)
		if !ok {
			continue
		}
		path, _ := patch["path"].(string)
		switch path {
		case "":
			switch value := patch["value"].(type) {
			case string:
				s.answer = value
			case []any:
				s.chunks = s.chunks[:0]
				for _, cAny := range value {
					c, _ := cAny.(string)
					s.chunks = append(s.chunks, c)
				}
			}
		default:
			if rest, ok := strings.CutPrefix(path, "/chunks/"); ok {
				var idx int
				if _, err := fmt.Sscanf(rest, "%d", &idx); err == nil && idx >= 0 {
					value, _ := patch["value"].(string)
					for len(s.chunks) <= idx {
						s.chunks = append(s.chunks, "")
					}
					s.chunks[idx] = value
				}
			}
		}
	}
}

// pplxExtractAnswerFromFinalText recovers the answer from the COMPLETED
// event's text when the stream never delivered markdown blocks: the text
// is a double-encoded FINAL step blob whose answer field is itself JSON
// with answer/chunks/structured_answer.
func pplxExtractAnswerFromFinalText(text string) (answer string, chunks []string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return "", nil, false
	}
	var step struct {
		StepType string `json:"step_type"`
		Content  struct {
			Answer string `json:"answer"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(trimmed), &step); err != nil || step.StepType != "FINAL" {
		return "", nil, false
	}
	inner := strings.TrimSpace(step.Content.Answer)
	if inner == "" {
		return "", nil, false
	}
	var obj struct {
		Answer           string   `json:"answer"`
		Chunks           []string `json:"chunks"`
		StructuredAnswer string   `json:"structured_answer"`
	}
	if err := json.Unmarshal([]byte(inner), &obj); err != nil {
		return inner, nil, true
	}
	switch {
	case len(obj.Chunks) > 0:
		return "", obj.Chunks, true
	case obj.Answer != "":
		return obj.Answer, nil, true
	case obj.StructuredAnswer != "":
		return obj.StructuredAnswer, nil, true
	}
	return inner, nil, true
}

// pplxIsAnswerUsage reports whether the event's usage marks answer text.
func pplxIsAnswerUsage(usage string) bool {
	if usage == "ask_text" {
		return true
	}
	if strings.HasPrefix(usage, "ask_text_") && strings.HasSuffix(usage, "_markdown") {
		return true
	}
	return strings.Contains(usage, "markdown")
}

// pplxExtractContent folds one SSE event into the state and returns what
// the client sees: a content delta, a thinking delta, and whether the turn
// is done. An error event terminates the turn with an error.
func (s *pplxExtractState) pplxExtractContent(event map[string]any) (contentDelta, thinkingDelta string, done bool, err error) {
	if code, _ := event["error_code"].(string); code != "" {
		msg, _ := event["error_message"].(string)
		return "", "", true, fmt.Errorf("%s: %s", code, msg)
	}
	if msg, _ := event["error_message"].(string); msg != "" {
		return "", "", true, fmt.Errorf("%s", msg)
	}
	if uuid, _ := event["backend_uuid"].(string); uuid != "" {
		s.backendUUID = uuid
	}
	if text, _ := event["text"].(string); text != "" {
		s.lastText = text
	}

	if thinking := s.extractThinking(event); thinking != "" {
		return "", thinking, false, nil
	}
	if thinking := s.extractPlanGoals(event); thinking != "" {
		return "", thinking, false, nil
	}

	// Materialized and diffed answer blocks land before the usage gate.
	if block, _ := event["block"].(map[string]any); block != nil {
		if workflow, ok := block["workflow_block"].(string); ok && workflow != "" {
			s.answer = workflow
		}
	}
	if block, _ := event["block"].(map[string]any); block != nil {
		if diffBlock, ok := block["diff_block"].(map[string]any); ok {
			if field, _ := diffBlock["field"].(string); field == "workflow_block" {
				if patches, ok := diffBlock["diff"].([]any); ok {
					s.applyMarkdownDiff(patches)
				}
			}
		}
	}

	usage, _ := event["usage"].(string)
	if pplxIsAnswerUsage(usage) {
		if markdown, ok := event["markdown_block"].(map[string]any); ok {
			if chunks, ok := markdown["chunks"].([]any); ok && len(chunks) > 0 {
				s.chunks = s.chunks[:0]
				for _, cAny := range chunks {
					c, _ := cAny.(string)
					s.chunks = append(s.chunks, c)
				}
			} else if answer, ok := markdown["answer"].(string); ok && answer != "" {
				s.answer = answer
			}
		} else if text, ok := event["text"].(string); ok && text != "" {
			s.answer = text
		}
	}

	// Legacy fallback: a plain non-JSON text payload with no structured
	// blocks is still the answer.
	if s.answer == "" && len(s.chunks) == 0 {
		if text, ok := event["text"].(string); ok && text != "" && usage == "" {
			s.answer = text
		}
	}

	if status, _ := event["status"].(string); status == "COMPLETED" {
		if answer, chunks, ok := pplxExtractAnswerFromFinalText(s.lastText); ok {
			if len(chunks) > 0 {
				s.chunks = chunks
			} else if answer != "" && len(s.currentAnswer()) < len(answer) {
				s.answer = answer
			}
		}
		s.fullAnswer = s.currentAnswer()
		return "", "", true, nil
	}

	return s.delta(), "", false, nil
}

// ——— auth + headers ———

// pplxCookieHeader normalizes the pasted credential into the Cookie header
// the endpoint expects: a bare session-token value is wrapped, a pasted
// header or pair blob passes through untouched.
func pplxCookieHeader(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if after, ok := strings.CutPrefix(lower, "bearer "); ok {
		_ = after
		trimmed = strings.TrimSpace(trimmed[len("bearer "):])
	}
	if after, ok := strings.CutPrefix(lower, "cookie:"); ok {
		_ = after
		trimmed = strings.TrimSpace(trimmed[len("cookie:"):])
	}
	if strings.Contains(trimmed, "__Secure-next-auth.session-token") {
		return trimmed
	}
	return "__Secure-next-auth.session-token=" + trimmed
}

func pplxHeaders(cookieHeader, requestID string) [][2]string {
	return [][2]string{
		{"Content-Type", "application/json"},
		{"Accept", "text/event-stream"},
		{"Origin", perplexityWebOrigin},
		{"Referer", perplexityWebOrigin + "/"},
		{"User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:146.0) Gecko/20100101 Firefox/146.0"},
		{"x-perplexity-request-endpoint", perplexityWebSSEPath},
		{"x-perplexity-request-reason", "ask-query-state-provider"},
		{"x-perplexity-request-try-number", "1"},
		{"x-request-id", requestID},
		{"Cookie", cookieHeader},
	}
}

// ——— the executor ———

func ForwardPerplexityWeb(w http.ResponseWriter, req *Request) error {
	ctx := req.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if !CheckWebExecutorAllowed(w, "perplexity-web") {
		return nil
	}

	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil || body == nil {
		return proxy.UpstreamFailure(http.StatusBadRequest, "perplexity-web: request body is not a JSON object")
	}
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return proxy.UpstreamFailure(http.StatusBadRequest, "perplexity-web: missing or empty messages array")
	}

	modelName, _ := body["model"].(string)
	route := pplxResolveModel(modelName)

	parsed := pplxParseOpenAIMessages(messages)
	toolsHint := pplxFormatToolsHint(body["tools"])
	followUpUUID := pplxSessionLookup(parsed.history)
	query := pplxBuildQuery(parsed, toolsHint, followUpUUID)
	if strings.TrimSpace(query) == "" {
		return proxy.UpstreamFailure(http.StatusBadRequest, "perplexity-web: empty query after processing")
	}

	cookieHeader := pplxCookieHeader(req.APIKey)
	if cookieHeader == "" {
		return proxy.UpstreamFailure(http.StatusUnauthorized,
			"perplexity-web needs a __Secure-next-auth.session-token cookie — paste the session-token value (or the whole Cookie header) from a logged-in perplexity.ai browser into the connection")
	}

	requestID := pplxUUID()
	headers := pplxHeaders(cookieHeader, requestID)
	payload := pplxBuildBody(query, parsed.current, route.mode, route.modelPref, followUpUUID, requestID)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return proxy.UpstreamFailure(http.StatusInternalServerError, "perplexity-web: marshal payload: "+err.Error())
	}

	log.Info("perplexity-web", "query", "model", modelName, "mode", route.mode, "pref", route.modelPref, "followup", followUpUUID != "", "chars", len(query))

	callCtx, cancel := context.WithTimeout(ctx, pplxRequestTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, perplexityWebChatURL, strings.NewReader(string(encoded)))
	if err != nil {
		return proxy.UpstreamFailure(http.StatusInternalServerError, "perplexity-web: build request: "+err.Error())
	}
	for _, h := range headers {
		httpReq.Header.Set(h[0], h[1])
	}

	client := req.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpReq)
	if err != nil {
		return proxy.UpstreamFailure(http.StatusBadGateway, "perplexity-web: connection failed: "+err.Error())
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return pplxStatusFailure(response.StatusCode, string(snippet))
	}

	responseID := "chatcmpl-pplx-" + pplxUUID()[:12]
	if req.IsStream {
		return pplxStreamToClient(w, req, response.Body, modelName, responseID, parsed, followUpUUID)
	}
	return pplxCollectToClient(w, req, response.Body, modelName, responseID, parsed, followUpUUID)
}

// pplxStatusFailure maps a refused turn onto the failure the router acts on,
// with the message the operator can do something about.
func pplxStatusFailure(status int, bodySnippet string) error {
	lower := strings.ToLower(bodySnippet)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		if strings.Contains(lower, "cloudflare") || strings.Contains(lower, "cf-mitigated") || strings.Contains(lower, "challenge") {
			return proxy.UpstreamFailure(status,
				"perplexity-web: Cloudflare blocked the request — the edge rejected this server's TLS fingerprint (common on VPS/datacenter IPs). Route perplexity-web through a residential proxy or re-capture the cookie from a browser on this network.")
		}
		return proxy.UpstreamFailure(status,
			"perplexity-web: auth failed — the session cookie may be expired. Re-paste your __Secure-next-auth.session-token from a logged-in perplexity.ai browser.")
	case http.StatusTooManyRequests:
		return proxy.UpstreamFailure(http.StatusTooManyRequests,
			"perplexity-web: rate limited by perplexity.ai. Wait a moment and retry, or switch model.")
	default:
		snippet := strings.TrimSpace(bodySnippet)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return proxy.UpstreamFailure(http.StatusBadGateway,
			fmt.Sprintf("perplexity-web: upstream answered %d: %s", status, snippet))
	}
}

// pplxStreamToClient relays the upstream event stream as OpenAI SSE chunks:
// a role chunk, thinking deltas under reasoning_content, content deltas
// through the response cleaner, and a terminal stop + [DONE].
func pplxStreamToClient(w http.ResponseWriter, req *Request, upstream io.Reader, model, responseID string, parsed pplxParsedMessages, followUpUUID string) error {
	hw := proxy.NewHeartbeatWriter(req.Ctx, w, 0)
	defer func() { _ = hw.Close() }()
	flusher := proxy.WriteSSEHeaders(hw)

	created := time.Now().Unix()
	startTime := req.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	var emitErr error
	emit := func(delta map[string]any, finish any) error {
		if emitErr != nil {
			return emitErr
		}
		if req.TTFT != nil && *req.TTFT == 0 {
			*req.TTFT = time.Since(startTime).Milliseconds()
		}
		chunk := map[string]any{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			emitErr = err
			return err
		}
		if _, err := hw.Write(fmt.Appendf(nil, "data: %s\n\n", encoded)); err != nil {
			emitErr = err
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	_ = emit(map[string]any{"role": "assistant"}, nil)

	state := &pplxExtractState{}
	scanErr := pplxScanSSE(upstream, func(event map[string]any) bool {
		contentDelta, thinkingDelta, done, err := state.pplxExtractContent(event)
		if err != nil {
			_ = emit(map[string]any{"content": "[Error: " + err.Error() + "]"}, nil)
			emitErr = fmt.Errorf("%s", err.Error())
			return false
		}
		if thinkingDelta != "" {
			_ = emit(map[string]any{"reasoning_content": thinkingDelta + "\n"}, nil)
		}
		if contentDelta != "" {
			_ = emit(map[string]any{"content": pplxCleanResponse(contentDelta, false)}, nil)
		}
		if done {
			return false
		}
		return true
	})
	if scanErr != nil && emitErr == nil {
		emitErr = scanErr
	}

	// The turn's ledger entry: the answer as it finally stood, so the next
	// request with the same history continues this backend session.
	answer := state.currentAnswer()
	pplxSessionStore(parsed.history, parsed.current, pplxCleanResponse(answer, true), state.backendUUID)

	if emitErr != nil {
		return proxy.UpstreamFailure(http.StatusBadGateway, "perplexity-web: upstream stream failed: "+emitErr.Error())
	}
	_ = emit(map[string]any{}, "stop")
	if _, err := hw.Write([]byte("data: [DONE]\n\n")); err == nil && flusher != nil {
		flusher.Flush()
	}
	return nil
}

// pplxCollectToClient buffers the upstream event stream into one
// chat.completion document, with the thinking narration under
// reasoning_content and an estimated usage block.
func pplxCollectToClient(w http.ResponseWriter, req *Request, upstream io.Reader, model, responseID string, parsed pplxParsedMessages, followUpUUID string) error {
	state := &pplxExtractState{}
	scanErr := pplxScanSSE(upstream, func(event map[string]any) bool {
		_, _, done, err := state.pplxExtractContent(event)
		if err != nil {
			return false
		}
		return !done
	})
	if scanErr != nil {
		return proxy.UpstreamFailure(http.StatusBadGateway, "perplexity-web: upstream stream failed: "+scanErr.Error())
	}

	answer := pplxCleanResponse(state.currentAnswer(), true)
	thinking := strings.Join(state.thinkingParts, "\n")
	pplxSessionStore(parsed.history, parsed.current, answer, state.backendUUID)

	message := map[string]any{"role": "assistant", "content": answer}
	if thinking != "" {
		message["reasoning_content"] = thinking
	}
	promptTokens := len(parsed.current) / 4
	if promptTokens == 0 {
		promptTokens = 1
	}
	completionTokens := len(answer) / 4
	completion := map[string]any{
		"id":      responseID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}
	handlerutil.WriteJSON(w, http.StatusOK, completion)
	return nil
}
