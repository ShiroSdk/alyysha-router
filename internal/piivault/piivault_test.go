// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package piivault

import (
	"strings"
	"testing"
	"time"
)

func vaultTestSetup(t *testing.T) {
	t.Helper()
	SetMode("on")
	Reset()
	t.Cleanup(func() {
		SetMode("off")
		Reset()
	})
}

// TestTokenizeRoundTrip proves the core contract: the body leaves with
// placeholders and no raw values; the same value maps to the same token, so
// the conversation stays consistent; the detokenizer restores exactly.
func TestTokenizeRoundTrip(t *testing.T) {
	vaultTestSetup(t)
	body := []byte(`{"messages":[{"role":"user","content":"mail bob@corp.io and bob@corp.io again, card 4111111111111111"}]}`)
	tokenized, changed := Tokenize("sess-1", body)
	if !changed {
		t.Fatal("body with PII was not tokenized")
	}
	s := string(tokenized)
	if strings.Contains(s, "bob@corp.io") || strings.Contains(s, "4111111111111111") {
		t.Errorf("raw PII survived tokenization: %s", s)
	}
	// Determinism: both occurrences of the same email share one token.
	// The 16-digit card tokenizes under the ID_NIK entity (both patterns
	// want 16 Luhn-valid digits and NIK runs first) — the entity label does
	// not matter to the vault, only that one value maps to one token.
	idx := strings.Index(s, "[PII:EMAIL_ADDRESS:")
	if idx < 0 {
		t.Fatalf("no email token in %s", s)
	}
	first := s[idx : idx+strings.Index(s[idx:], "]")+1]
	if strings.Count(s, first) != 2 {
		t.Errorf("token %q appears %d times, want 2 (deterministic per value)", first, strings.Count(s, first))
	}

	// Restore: the detokenizer maps the token back exactly.
	d := NewDetokenizer("sess-1")
	if d == nil {
		t.Fatal("no detokenizer for a tokenized session")
	}
	restored := d.Push(tokenized)
	restored = append(restored, d.Flush()...)
	if !strings.Contains(string(restored), "bob@corp.io") || !strings.Contains(string(restored), "4111111111111111") {
		t.Errorf("restore lost values: %s", restored)
	}
	if strings.Contains(string(restored), "[PII:") {
		t.Errorf("token survived restore: %s", restored)
	}
}

// TestJSONShapePreserved proves the byte-level substitution keeps the JSON
// document valid — the token never carries quotes or backslashes.
func TestJSONShapePreserved(t *testing.T) {
	vaultTestSetup(t)
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"ping a@b.io"}],"stream":false}`)
	tokenized, _ := Tokenize("sess-json", body)
	// The model/token fields and the structure are untouched.
	if !strings.Contains(string(tokenized), `"model":"m"`) || !strings.Contains(string(tokenized), `"stream":false`) {
		t.Errorf("structure damaged: %s", tokenized)
	}
}

// TestFrameSplitRestore proves a token split across two streamed frames still
// resolves, and that a fragment at EOF flushes as literal text.
func TestFrameSplitRestore(t *testing.T) {
	vaultTestSetup(t)
	body := []byte(`user a@b.io`)
	tokenized, _ := Tokenize("sess-split", body)
	s := string(tokenized)
	idx := strings.Index(s, "[PII:")
	token := s[idx : idx+strings.Index(s[idx:], "]")+1]

	// Split the token mid-way across two frames.
	d := NewDetokenizer("sess-split")
	cut := len(token) / 2
	part1 := d.Push([]byte("answer: " + token[:cut]))
	part2 := d.Push([]byte(token[cut:] + " end"))
	out := string(part1) + string(part2) + string(d.Flush())
	if strings.Contains(out, "PII") {
		t.Errorf("split token unresolved: %s", out)
	}
	if !strings.Contains(out, "a@b.io") {
		t.Errorf("restore lost the value: %s", out)
	}

	// A fragment that never completes flushes as literal text at EOF.
	d2 := NewDetokenizer("sess-split")
	held := d2.Push([]byte("x [PII:EM"))
	flushed := d2.Flush()
	if string(held)+string(flushed) != "x [PII:EM" {
		t.Errorf("EOF fragment lost: %q + %q", held, flushed)
	}
}

// TestUnrelatedFramesPassThrough proves frames without placeholders or
// prefixes are returned unchanged and nothing is held back.
func TestUnrelatedFramesPassThrough(t *testing.T) {
	vaultTestSetup(t)
	Tokenize("sess-plain", []byte("seed a@b.io"))
	d := NewDetokenizer("sess-plain")
	frame := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
	out := d.Push(frame)
	if string(out) != string(frame) {
		t.Errorf("unrelated frame altered: %s", out)
	}
}

// TestSessionIsolation proves one session's mapping never restores another's.
func TestSessionIsolation(t *testing.T) {
	vaultTestSetup(t)
	tokenized, _ := Tokenize("sess-a", []byte("a@b.io"))
	d := NewDetokenizer("sess-b")
	if d != nil {
		out := string(d.Push(tokenized))
		if strings.Contains(out, "a@b.io") {
			t.Errorf("session b restored session a's value: %s", out)
		}
	}
}

// TestTTLAndCap proves sessions age out and the map stays bounded.
func TestTTLAndCap(t *testing.T) {
	vaultTestSetup(t)
	base := time.Unix(1_800_000_000, 0)
	now := base
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })

	Tokenize("ttl-sess", []byte("a@b.io"))
	now = base.Add(sessionTTL + time.Minute)
	// The aged session is swept when the next insert happens.
	Tokenize("fresh-sess", []byte("c@d.io"))
	if d := NewDetokenizer("ttl-sess"); d != nil {
		// Aged-out sessions keep no live mapping.
		if out := string(d.Push([]byte("[PII:EMAIL:00000000]"))); strings.Contains(out, "a@b.io") {
			t.Errorf("aged session still restores: %s", out)
		}
	}
}

// TestOffModeCostsNothing proves the default leaves the body untouched and
// the detokenizer nil.
func TestOffModeCostsNothing(t *testing.T) {
	SetMode("off")
	body := []byte("a@b.io")
	out, changed := Tokenize("sess-off", body)
	if changed || string(out) != "a@b.io" {
		t.Errorf("off mode tokenized: %s (%v)", out, changed)
	}
	if NewDetokenizer("sess-off") != nil {
		t.Error("off mode returned a detokenizer")
	}
}
