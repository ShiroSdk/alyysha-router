// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

import (
	"sync"
	"time"
)

// RoutingExplain is one routing decision the dashboard can show: a candidate
// that was considered (attempted or skipped) for a turn, with the reason it
// was chosen or passed over. The chain from G1.1/G1.2/G1.3 feeds the reason.
type RoutingExplain struct {
	Timestamp    string `json:"timestamp"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	ConnectionID string `json:"connectionId,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	Reason       string `json:"reason"`
	Outcome      string `json:"outcome"` // "attempted" | "success" | "skipped"
	LatencyMs    int64  `json:"latencyMs,omitempty"`
}

// routingExplainRing is the in-memory store — 200 entries, newest last, no
// SQLite: this is disposable observability, not a ledger.
var (
	routingExplainMu    sync.Mutex
	routingExplainRing  []RoutingExplain
	routingExplainLimit = 200
)

// RecordRoutingExplain appends one entry, dropping the oldest beyond 200.
func RecordRoutingExplain(entry RoutingExplain) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	routingExplainMu.Lock()
	defer routingExplainMu.Unlock()
	routingExplainRing = append(routingExplainRing, entry)
	if len(routingExplainRing) > routingExplainLimit {
		routingExplainRing = routingExplainRing[len(routingExplainRing)-routingExplainLimit:]
	}
}

// RoutingExplainSnapshot returns up to limit newest entries, oldest first.
func RoutingExplainSnapshot(limit int) []RoutingExplain {
	routingExplainMu.Lock()
	defer routingExplainMu.Unlock()
	if limit <= 0 || limit > routingExplainLimit {
		limit = 20
	}
	if len(routingExplainRing) == 0 {
		return []RoutingExplain{}
	}
	start := len(routingExplainRing) - limit
	if start < 0 {
		start = 0
	}
	out := make([]RoutingExplain, limit)
	copy(out, routingExplainRing[start:])
	return out[:len(routingExplainRing)-start]
}

// RoutingExplainFromRecorder copies a request's fallback attempts into the
// ring as attempted/success entries — one call at request end covers every
// step the chain took.
func RoutingExplainFromRecorder(rec *FallbackAttemptRecorder) {
	for _, a := range rec.Snapshot() {
		outcome := "attempted"
		if a.StatusCode >= 200 && a.StatusCode < 300 || (a.StatusCode == 0 && a.Error == "") {
			outcome = "success"
		}
		RecordRoutingExplain(RoutingExplain{
			Provider:     a.Provider,
			Model:        a.Model,
			ConnectionID: a.ConnectionID,
			StatusCode:   a.StatusCode,
			Outcome:      outcome,
			Reason:       attemptReason(a),
			LatencyMs:    a.LatencyMs,
		})
	}
}

// attemptReason names why the attempt ended this way — the chain states the
// fallback machinery already computes.
func attemptReason(a FallbackAttempt) string {
	switch {
	case a.StatusCode == 0 && a.Error == "":
		return "selected (rotation order)"
	case a.StatusCode == 429:
		return "rate limited; moved to the next candidate"
	case a.StatusCode >= 500:
		return "upstream error; moved to the next candidate"
	default:
		if a.Error != "" {
			return "error: " + a.Error
		}
		return "upstream status; moved to the next candidate"
	}
}
