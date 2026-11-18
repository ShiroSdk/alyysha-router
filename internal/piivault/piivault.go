// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
// Package piivault implements the PII vault (N6): with the vault armed, the
// gateway replaces personal data in the INBOUND request with deterministic
// placeholders — [PII:EMAIL:ab12cd34] — so the model and the upstream
// provider never see the raw values, and restores the originals on the way
// back out, buffered responses and streamed frames alike. The client's view
// is unchanged; the provider's is redacted.
//
// Tokens are deterministic per (entity type, value): the same email that
// appears twice in one session tokenizes identically, so the model's replies
// and the conversation history stay internally consistent, and a value that
// re-appears in a later turn maps back to the same entry. The mapping lives
// in memory only, keyed by the client session, aged out on a TTL — the vault
// is a redaction window, not a store anyone bills from or audits.
//
// chat.pii_vault = off (default) | on. Off costs nothing: both lanes check
// the switch before touching the body.
package piivault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"9routerx/proxy/internal/guardrails"
)

const (
	sessionTTL    = 30 * time.Minute
	maxSessions   = 2048
	maxPerSession = 256

	// tokenPrefix opens every placeholder. The bracketed, namespaced shape is
	// chosen so a model quoting it back is still an exact byte string the
	// detokenizer can find, and so it cannot collide with ordinary text.
	tokenPrefix = "[PII:"
	tokenSuffix = "]"
	// tokenValueLen is the hex length of the value digest inside a token.
	tokenValueLen = 8
	// maxCarryLen bounds how many trailing bytes the streaming detokenizer
	// may hold back waiting for a token to complete across a frame boundary.
	maxCarryLen = len(tokenPrefix) + len("IP_ADDRESS") + tokenValueLen + len(tokenSuffix) - 1
)

var (
	mode       atomic.Value // string: "off" | "on"
	sessionsMu sync.Mutex
	sessions   map[string]*vaultSession
	clock      = time.Now // test hook
)

type vaultSession struct {
	entries map[string]string // token -> original value
	touched time.Time
}

// tokenRe matches a whole placeholder in model output or client history.
var tokenRe = regexp.MustCompile(`\[PII:[A-Z_]+:[0-9a-f]{` + fmt.Sprint(tokenValueLen) + `}\]`)

func init() {
	mode.Store("off")
	sessions = make(map[string]*vaultSession)
}

// SetMode applies the stored setting. Unknown values read as off.
func SetMode(m string) {
	if m == "on" {
		mode.Store("on")
		return
	}
	mode.Store("off")
}

func armed() bool {
	v, _ := mode.Load().(string)
	return v == "on"
}

// Tokenize replaces every PII match in the request body with a deterministic
// placeholder, recording the mapping under the session. The body is treated
// as bytes: the placeholders are JSON-safe (no quotes, no backslashes), so a
// byte-level substitution inside a JSON string preserves the document, and a
// match outside one is impossible for these patterns — an email or a card
// number cannot be a JSON structural token. The second return reports
// whether anything changed, so the caller can skip the rewrite.
func Tokenize(sessionID string, body []byte) ([]byte, bool) {
	if !armed() || sessionID == "" || len(body) == 0 {
		return body, false
	}
	findings := guardrails.ScanPIIText(string(body))
	if len(findings) == 0 {
		return body, false
	}

	now := clock()
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if len(sessions) >= maxSessions {
		sweepLocked(now)
	}
	entries := sessionLocked(sessionID, now)

	out := make([]byte, 0, len(body)+len(findings)*8)
	last := 0
	for _, f := range findings {
		if f.Start < last || f.End > len(body) {
			continue
		}
		value := string(body[f.Start:f.End])
		token := tokenFor(f.Entity, value)
		if orig, seen := entries[token]; !seen {
			if len(entries) >= maxPerSession {
				// Keep the mapping bounded per session: a session that
				// introduces more distinct values than the cap leaves the
				// excess untokenized rather than evicting mappings the model
				// may already be using.
				out = append(out, body[last:f.End]...)
				last = f.End
				continue
			}
			entries[token] = value
		} else if orig != value {
			// Hash collision across distinct values: leave this one raw
			// rather than restore the wrong value on the way back.
			out = append(out, body[last:f.End]...)
			last = f.End
			continue
		}
		out = append(out, body[last:f.Start]...)
		out = append(out, token...)
		last = f.End
	}
	out = append(out, body[last:]...)
	return out, true
}

// Detokenizer restores placeholders in an outbound stream. Frames are pushed
// one at a time; a placeholder split across a frame boundary is held back in
// a carry buffer and completed on the next push, so a token the model emits
// straddling two SSE chunks still resolves. Flush emits whatever the carry
// holds when the stream ends.
type Detokenizer struct {
	entries map[string]string
	carry   []byte
}

// NewDetokenizer returns a detokenizer for the session, or nil when the
// vault is off or the session has no mapping — the nil case is the hot path
// for every deployment that has not armed the feature.
func NewDetokenizer(sessionID string) *Detokenizer {
	if !armed() || sessionID == "" {
		return nil
	}
	sessionsMu.Lock()
	sess, ok := sessions[sessionID]
	sessionsMu.Unlock()
	if !ok || len(sess.entries) == 0 {
		return nil
	}
	// Copy the mapping: the session map is mutable while the response
	// streams, and a mid-stream eviction must not corrupt a restore.
	copied := make(map[string]string, len(sess.entries))
	for k, v := range sess.entries {
		copied[k] = v
	}
	return &Detokenizer{entries: copied}
}

// Push runs one outbound frame through the detokenizer and returns the bytes
// that are safe to write: frames with no placeholder pass through whole, and
// only a trailing candidate token fragment is held back. The originals never
// contain quotes or backslashes (they are emails, IPs, card numbers, IBANs,
// NIKs), so a byte-level substitution inside a JSON string stays valid JSON.
func (d *Detokenizer) Push(frame []byte) []byte {
	if d == nil || len(frame) == 0 {
		return frame
	}
	buf := append(d.carry, frame...)
	d.carry = nil
	out := tokenRe.ReplaceAllFunc(buf, func(m []byte) []byte {
		if v, ok := d.entries[string(m)]; ok {
			return []byte(v)
		}
		return m
	})
	// A trailing fragment that could still grow into a token is held back
	// until the next push; Flush emits it at end of stream.
	if tail := trailingTokenPrefix(out); tail > 0 {
		d.carry = append(d.carry, out[len(out)-tail:]...)
		out = out[:len(out)-tail]
	}
	return out
}

// Flush emits the carried bytes when the stream ends. An incomplete token
// prefix at EOF is not a token; it passes through as the literal text the
// model produced.
func (d *Detokenizer) Flush() []byte {
	if d == nil || len(d.carry) == 0 {
		return nil
	}
	out := d.carry
	d.carry = nil
	return out
}

// trailingTokenPrefix reports how many trailing bytes of buf are a prefix of
// some placeholder. A lone "[" is far too common to hold back on, so the
// minimum fragment is two bytes.
func trailingTokenPrefix(buf []byte) int {
	if len(buf) > maxCarryLen {
		buf = buf[len(buf)-maxCarryLen:]
	}
	for n := len(buf); n > 1; n-- {
		if strings.HasPrefix(string(buf[len(buf)-n:]), tokenPrefix) {
			return n
		}
	}
	return 0
}

// tokenFor builds the deterministic placeholder for one value.
func tokenFor(entity, value string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return fmt.Sprintf("%s%s:%s%s", tokenPrefix, entity, hex.EncodeToString(sum[:])[:tokenValueLen], tokenSuffix)
}

// sessionLocked returns the session's entry map, evicting to stay bounded.
// Caller holds the lock.
func sessionLocked(sessionID string, now time.Time) map[string]string {
	if sess, ok := sessions[sessionID]; ok {
		sess.touched = now
		return sess.entries
	}
	if len(sessions) >= maxSessions {
		// TTL sweep first; a wholesale drop only when the cap is still under
		// pressure, because this is a redaction window, not a quota.
		sweepLocked(now)
		for id := range sessions {
			if len(sessions) < maxSessions {
				break
			}
			delete(sessions, id)
		}
	}
	sess := &vaultSession{entries: make(map[string]string), touched: now}
	sessions[sessionID] = sess
	return sess.entries
}

// sweepLocked drops sessions the TTL has aged out. Caller holds the lock.
func sweepLocked(now time.Time) {
	for id, sess := range sessions {
		if now.Sub(sess.touched) > sessionTTL {
			delete(sessions, id)
		}
	}
}

// Reset clears the vault. Tests use it.
func Reset() {
	sessionsMu.Lock()
	sessions = make(map[string]*vaultSession)
	sessionsMu.Unlock()
}
