// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"9routerx/proxy/internal/handlerutil"
)

// toolLoopTestSetup arms the watchdog for one test and puts every knob back
// to the shipping default afterwards: the watchdog state is package-global,
// and a test that left break mode on would withhold tool calls from every
// test running after it.
func toolLoopTestSetup(t *testing.T, mode string) {
	t.Helper()
	SetToolLoopWatch(mode)
	resetToolLoopWatch()
	t.Cleanup(func() {
		SetToolLoopWatch("off")
		SetToolLoopThreshold(toolLoopDefaultThreshold)
		resetToolLoopWatch()
	})
}

func toolCall(name, args string) []map[string]any {
	return []map[string]any{{
		"function": map[string]any{"name": name, "arguments": args},
	}}
}

func TestToolLoopSignature(t *testing.T) {
	if got := toolLoopSignature(nil); got != "" {
		t.Errorf("no tool calls gave %q, want \"\"", got)
	}
	a := toolLoopSignature(toolCall("read_file", `{"path":"a.go"}`))
	if a == "" {
		t.Fatal("a tool call produced no signature")
	}
	same := toolLoopSignature(toolCall("read_file", `{"path":"a.go"}`))
	if a != same {
		t.Errorf("identical calls disagree: %q vs %q", a, same)
	}
	otherArgs := toolLoopSignature(toolCall("read_file", `{"path":"b.go"}`))
	if a == otherArgs {
		t.Error("different arguments produced the same signature")
	}
	otherName := toolLoopSignature(toolCall("write_file", `{"path":"a.go"}`))
	if a == otherName {
		t.Error("different tool names produced the same signature")
	}
}

func TestToolLoopBreakAtThreshold(t *testing.T) {
	toolLoopTestSetup(t, "break")
	sig := toolLoopSignature(toolCall("run", `{"cmd":"ls"}`))

	if a := toolLoopCheck("sess-break", sig); a != "" {
		t.Errorf("first repeat: %q, want \"\"", a)
	}
	if a := toolLoopCheck("sess-break", sig); a != "" {
		t.Errorf("second repeat: %q, want \"\"", a)
	}
	if a := toolLoopCheck("sess-break", sig); a != "break" {
		t.Errorf("third repeat: %q, want break", a)
	}
	// A break keeps the session record: an agent that retried without
	// changing anything is still in the loop, so the next identical call
	// breaks immediately instead of waiting another threshold repeats.
	if a := toolLoopCheck("sess-break", sig); a != "break" {
		t.Errorf("repeat after break: %q, want break", a)
	}
	// A different call means the agent moved on; the counter resets and the
	// threshold starts over.
	other := toolLoopSignature(toolCall("run", `{"cmd":"pwd"}`))
	if a := toolLoopCheck("sess-break", other); a != "" {
		t.Errorf("changed call after break: %q, want \"\"", a)
	}
}

func TestToolLoopOffModeTracksNothing(t *testing.T) {
	toolLoopTestSetup(t, "off")
	sig := toolLoopSignature(toolCall("run", `{"cmd":"ls"}`))
	for i := 0; i < 10; i++ {
		if a := toolLoopCheck("sess-off", sig); a != "" {
			t.Fatalf("off mode intervened at repeat %d: %q", i+1, a)
		}
	}
	toolLoopSessionsMu.Lock()
	n := len(toolLoopSessions)
	toolLoopSessionsMu.Unlock()
	if n != 0 {
		t.Errorf("off mode tracked %d sessions, want 0", n)
	}
}

func TestToolLoopLogModeRecordsButPasses(t *testing.T) {
	toolLoopTestSetup(t, "log")
	sig := toolLoopSignature(toolCall("run", `{"cmd":"ls"}`))
	for i := 0; i < 5; i++ {
		a := toolLoopCheck("sess-log", sig)
		if a == "break" {
			t.Fatal("log mode broke the loop")
		}
		if i >= 2 && a != "log" {
			t.Errorf("repeat %d: %q, want log", i+1, a)
		}
	}
}

func TestToolLoopThresholdConfigurable(t *testing.T) {
	toolLoopTestSetup(t, "break")
	SetToolLoopThreshold(2)
	sig := toolLoopSignature(toolCall("run", `{"cmd":"ls"}`))
	if a := toolLoopCheck("sess-thr", sig); a != "" {
		t.Errorf("first repeat: %q, want \"\"", a)
	}
	if a := toolLoopCheck("sess-thr", sig); a != "break" {
		t.Errorf("second repeat: %q, want break", a)
	}
	// Zero or negative keeps the previous value: a threshold that fires on
	// the first response would withhold every tool call.
	SetToolLoopThreshold(0)
	if a := toolLoopCheck("sess-thr", toolLoopSignature(toolCall("run", `{"cmd":"pwd"}`))); a != "" {
		t.Errorf("changed call after threshold-0 set: %q, want \"\"", a)
	}
}

func TestToolLoopSessionTTL(t *testing.T) {
	toolLoopTestSetup(t, "break")
	base := time.Unix(1_700_000_000, 0)
	now := base
	toolLoopClock = func() time.Time { return now }
	t.Cleanup(func() { toolLoopClock = time.Now })

	sig := toolLoopSignature(toolCall("run", `{"cmd":"ls"}`))
	_ = toolLoopCheck("sess-ttl", sig)
	_ = toolLoopCheck("sess-ttl", sig)
	now = base.Add(toolLoopSessionTTL + time.Minute)
	// The session aged out, so the count starts over and the threshold has
	// not been reached again.
	if a := toolLoopCheck("sess-ttl", sig); a != "" {
		t.Errorf("stale session broke immediately: %q, want \"\"", a)
	}
}

func TestToolLoopIndependentSessions(t *testing.T) {
	toolLoopTestSetup(t, "break")
	sig := toolLoopSignature(toolCall("run", `{"cmd":"ls"}`))
	_ = toolLoopCheck("sess-a", sig)
	_ = toolLoopCheck("sess-a", sig)
	// Session b has its own counter; one repeat is nowhere near the break.
	if a := toolLoopCheck("sess-b", sig); a != "" {
		t.Errorf("session b inherited session a's count: %q", a)
	}
	if a := toolLoopCheck("sess-a", sig); a != "break" {
		t.Errorf("session a: %q, want break", a)
	}
}

func TestToolLoopConcurrentChecks(t *testing.T) {
	toolLoopTestSetup(t, "break")
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			session := "sess-conc-" + strconv.Itoa(g%4)
			for i := 0; i < 50; i++ {
				_ = toolLoopCheck(session, toolLoopSignature(toolCall("run", `{"g":1}`)))
			}
		}(g)
	}
	wg.Wait()
}

func TestToolLoopStreamAccSplitDeltas(t *testing.T) {
	var acc toolLoopStreamAcc
	acc.armed = true
	acc.Feed([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"run"}}]}}]}`))
	acc.Feed([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"_tool","arguments":"{\"cmd\":"}}]}}]}`))
	acc.Feed([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}`))
	sig := acc.Signature()
	want := toolLoopSignature(toolCall("run_tool", `{"cmd":"ls"}`))
	if sig != want {
		t.Errorf("stream signature %q, want buffered-equivalent %q", sig, want)
	}
	// Frames that carry no tool call must not disturb the accumulation, and
	// the terminator must not either.
	acc.Feed([]byte(`data: {"choices":[{"delta":{"content":"hi"}}]}`))
	acc.Feed([]byte(`data: [DONE]`))
	acc.Feed([]byte(`not json at all`))
	if got := acc.Signature(); got != want {
		t.Errorf("signature changed after non-tool frames: %q", got)
	}
}

func TestToolLoopStreamAccDisarmed(t *testing.T) {
	var acc toolLoopStreamAcc
	acc.Feed([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"run"}}]}}]}`))
	if sig := acc.Signature(); sig != "" {
		t.Errorf("disarmed accumulator tracked %q", sig)
	}
}

func TestToolLoopInspectBuffered(t *testing.T) {
	toolLoopTestSetup(t, "break")
	ctx := handlerutil.WithSessionID(context.Background(), "sess-inspect")
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"run","arguments":"{\"cmd\":\"ls\"}"}}]},"finish_reason":"tool_calls"}]}`)

	if a := toolLoopInspectBuffered(ctx, body); a != "" {
		t.Errorf("first body: %q, want \"\"", a)
	}
	_ = toolLoopInspectBuffered(ctx, body)
	if a := toolLoopInspectBuffered(ctx, body); a != "break" {
		t.Errorf("third body: %q, want break", a)
	}
	// No tool calls, no signature, no intervention.
	plain := []byte(`{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	if a := toolLoopInspectBuffered(ctx, plain); a != "" {
		t.Errorf("plain body: %q, want \"\"", a)
	}
	// A malformed body must never reach the check.
	if a := toolLoopInspectBuffered(ctx, []byte(`{"choices":`)); a != "" {
		t.Errorf("malformed body: %q, want \"\"", a)
	}
	// No session in the context, no intervention either.
	if a := toolLoopInspectBuffered(context.Background(), body); a != "" {
		t.Errorf("sessionless context: %q, want \"\"", a)
	}
}

func TestWriteToolLoopBreak(t *testing.T) {
	rec := httptest.NewRecorder()
	writeToolLoopBreak(rec, false)
	if rec.Code != 429 {
		t.Errorf("openai break status %d, want 429", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	if body := rec.Body.String(); !contains(body, `"type":"tool_loop_break"`) || !contains(body, "tool-loop guard") {
		t.Errorf("openai body %q", body)
	}

	rec = httptest.NewRecorder()
	writeToolLoopBreak(rec, true)
	if rec.Code != 429 {
		t.Errorf("claude break status %d, want 429", rec.Code)
	}
	if body := rec.Body.String(); !contains(body, `"type":"error"`) || !contains(body, "tool_loop_break") {
		t.Errorf("claude body %q", body)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
