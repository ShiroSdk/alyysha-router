// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package guardrails

import (
	json "encoding/json/v2"
	"strings"
	"testing"
)

const widgetSchema = `{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"},"count":{"type":"number"}}}`

func schemaEngine(action Action) *Engine {
	return NewEngineWithCustom([]string{"json_schema"}, action, []CustomPattern{
		{Name: "json_schema", Regex: widgetSchema},
	})
}

// TestJSONSchemaDetectorMismatchLogged pins the detector: a response missing
// the required property and carrying a wrong-typed field produces a
// json_schema finding at the engine's action.
func TestJSONSchemaDetectorMismatchLogged(t *testing.T) {
	engine := schemaEngine(ActionLogOnly)
	payload := map[string]any{"answer": 42, "extra": "fine"}
	scanned, decision := engine.ScanJSON(payload)
	if decision.Action != ActionLogOnly || len(decision.Findings) == 0 {
		t.Fatalf("decision = %+v, want log_only with findings", decision)
	}
	found := false
	for _, f := range decision.Findings {
		if f.Detector == "json_schema" {
			found = true
			if !strings.Contains(f.Entity, "expected") {
				t.Errorf("finding entity = %q, want mismatch paths", f.Entity)
			}
		}
	}
	if !found {
		t.Error("no json_schema finding among the results")
	}
	// ScanJSON never mutates for schema mismatches — the payload passes whole.
	scannedMap := scanned.(map[string]any)
	if scannedMap["answer"] != 42 {
		t.Errorf("answer = %v, want the original 42 (schema findings never mutate)", scannedMap["answer"])
	}
}

// TestJSONSchemaDetectorBlock pins block action: the decision blocks so the
// buffered caller truncates the non-streaming response with a generic error.
func TestJSONSchemaDetectorBlock(t *testing.T) {
	engine := schemaEngine(ActionBlock)
	_, decision := engine.ScanJSON(map[string]any{"wrong": "shape"})
	if !decision.Blocked() {
		t.Errorf("decision = %v, want block", decision.Action)
	}
}

// TestJSONSchemaValidResponsePasses pins the pass path.
func TestJSONSchemaValidResponsePasses(t *testing.T) {
	engine := schemaEngine(ActionBlock)
	_, decision := engine.ScanJSON(map[string]any{"answer": "the answer", "count": 3.0})
	if decision.Blocked() || len(decision.Findings) != 0 {
		t.Errorf("valid response flagged: %+v", decision)
	}
}

// TestJSONSchemaResponseBodyBlock pins the buffered response path: a body
// whose JSON violates the schema is refused (ScanResponseBody returns the
// block decision), and a streaming-style body (not JSON) is untouched.
func TestJSONSchemaResponseBodyBlock(t *testing.T) {
	engine := schemaEngine(ActionBlock)

	blockedBody := []byte(`{"wrong":true}`)
	_, decision := ScanResponseBody(engine, blockedBody)
	if !decision.Blocked() {
		t.Errorf("mismatched body decision = %v, want block", decision.Action)
	}

	plain := []byte("plain text, not json at all")
	_, decision = ScanResponseBody(engine, plain)
	if decision.Blocked() {
		t.Error("non-JSON body must not be schema-validated")
	}
}

// TestSchemaParseBadJSON tolerates a malformed schema: it is skipped at
// registration, not a panic.
func TestSchemaParseBadJSON(t *testing.T) {
	engine := NewEngineWithCustom([]string{"json_schema"}, ActionBlock, []CustomPattern{
		{Name: "json_schema", Regex: "{not-json"},
	})
	if len(engine.jsonSchemas) != 0 {
		t.Errorf("malformed schema registered %d nodes, want 0", len(engine.jsonSchemas))
	}
	// The engine still runs with zero schemas.
	_, decision := engine.ScanJSON(map[string]any{"a": 1})
	if decision.Blocked() {
		t.Error("engine with no schemas must allow")
	}
	_ = json.Marshal
}
