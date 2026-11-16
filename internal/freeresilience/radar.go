// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package freeresilience

import (
	"log"
	"sort"
	"time"
)

// RadarRow is one provider's 24-hour health-radar aggregation: the numbers
// the fallback ranking and the dashboard radar read. Success rate doubles as
// uptime (an outcome either served the request or did not).
type RadarRow struct {
	Provider    string  `json:"provider"`
	Samples     int     `json:"samples"`
	SuccessRate float64 `json:"successRate"` // -1 = no data in window
	P50Ms       float64 `json:"p50Ms"`
	P95Ms       float64 `json:"p95Ms"`
	UptimePct   float64 `json:"uptimePct"` // -1 = no data in window
}

// RadarSnapshot aggregates every tracked provider's outcome ring into radar
// rows: samples, success rate, p50/p95 latency. Providers with no outcomes
// still appear (no data), matching the health snapshot's convention.
func (t *Tracker) RadarSnapshot() []RadarRow {
	if t == nil {
		return nil
	}
	now := t.now().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	providersSet := map[string]bool{}
	for pid := range t.health {
		providersSet[pid] = true
	}
	rows := make([]RadarRow, 0, len(providersSet))
	for pid := range providersSet {
		ring := pruneOutcomes(t.health[pid], now)
		row := RadarRow{Provider: pid, SuccessRate: -1, UptimePct: -1}
		row.Samples = len(ring)
		if len(ring) > 0 {
			var success int
			latencies := make([]float64, 0, len(ring))
			for _, o := range ring {
				if o.Success {
					success++
				}
				if o.LatencyMs > 0 {
					latencies = append(latencies, float64(o.LatencyMs))
				}
			}
			row.SuccessRate = float64(success) / float64(len(ring))
			row.UptimePct = row.SuccessRate * 100
			if len(latencies) > 0 {
				sort.Float64s(latencies)
				row.P50Ms = percentileOf(latencies, 0.50)
				row.P95Ms = percentileOf(latencies, 0.95)
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Provider < rows[j].Provider })
	return rows
}

// percentileOf is the nearest-rank percentile over pre-sorted samples.
func percentileOf(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(pct)*float64(len(sorted))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// CombinedHealthScore is the chain tie-breaker: the health score (success
// rate, cooling last) blended with a latency factor, so among proven
// providers the faster p50 ranks first. Cooling still sorts last and a
// provider without data stays neutral — user config order still wins above
// all of this.
func (t *Tracker) CombinedHealthScore(provider string) float64 {
	if t == nil {
		return 0
	}
	base := t.HealthScore(provider)
	if base <= 0 {
		// Cooling or empty: keep the base semantics (sort last / neutral).
		return base
	}
	rows := t.RadarSnapshot()
	for _, row := range rows {
		if row.Provider != provider || row.Samples == 0 {
			continue
		}
		// Latency factor: 30s p50 reads as 0, instant reads as 1.
		latencyFactor := 1 - min(row.P50Ms/30_000, 1)
		return base*0.8 + latencyFactor*0.2
	}
	return base
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// RadarStore is the optional persistence the flush pass uses — implemented
// by the SQLite repo; test fakes without it simply skip radar persistence.
type RadarStore interface {
	SaveFreeProviderRadar(rows []RadarRow, now time.Time) error
}

// FlushRadar persists the radar snapshot. Called by the scheduler's flush
// pass, next to the outcome-ring flush.
func (t *Tracker) FlushRadar() {
	if t == nil || t.store == nil {
		return
	}
	radarStore, ok := t.store.(RadarStore)
	if !ok {
		return
	}
	if err := radarStore.SaveFreeProviderRadar(t.RadarSnapshot(), t.now().UTC()); err != nil {
		log.Printf("[freeresilience] flush radar failed: %v", err)
	}
}
