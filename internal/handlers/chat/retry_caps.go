// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

import (
	"sync/atomic"

	"alyysha/proxy/internal/proxy"
)

// Attempt caps for the fallback chain, configurable through the settings key
// retry.attempt_caps = {"exact":N,"provider":N,"global":N} (all optional):
//
//	exact    — cap per body-hash ACROSS requests (a client retrying the same
//	           body does not spend the ladder twice); 0 disables the level.
//	provider — cap per provider for one body-hash; 0 disables the level.
//	global   — the per-request recorder cap (baseline: maxFallbackAttempts).
//
// Precedence is exact > provider > global; every default keeps the old
// behavior byte-identical.
type retryAttemptCaps struct {
	exact    atomic.Int32
	provider atomic.Int32
	global   atomic.Int32
}

var retryCaps retryAttemptCaps

func init() {
	retryCaps.global.Store(maxFallbackAttempts)
}

// SetRetryAttemptCaps applies the stored settings values. Non-positive or
// missing levels keep their defaults (exact/provider off, global = baseline).
func SetRetryAttemptCaps(exact, provider, global int) {
	if exact > 0 {
		retryCaps.exact.Store(int32(exact))
	} else {
		retryCaps.exact.Store(0)
	}
	if provider > 0 {
		retryCaps.provider.Store(int32(provider))
	} else {
		retryCaps.provider.Store(0)
	}
	if global > 0 {
		retryCaps.global.Store(int32(global))
	} else {
		retryCaps.global.Store(maxFallbackAttempts)
	}
}

type resolvedAttemptCaps struct {
	exact, provider, global int
}

func resolvedRetryAttemptCaps() resolvedAttemptCaps {
	return resolvedAttemptCaps{
		exact:    int(retryCaps.exact.Load()),
		provider: int(retryCaps.provider.Load()),
		global:   int(retryCaps.global.Load()),
	}
}

// fallbackAttemptBudget computes the recorder cap for one request: the
// resolved cap minus what the same body (and provider) already spent in
// recent attempts, floored at zero. exhausted=true means the ladder must not
// run — the body burned its budget moments ago.
func fallbackAttemptBudget(body []byte, provider string) (budget int, exhausted bool) {
	caps := resolvedRetryAttemptCaps()
	effective := caps.global
	spent := proxy.BodyAttemptsSpent(body)
	switch {
	case caps.exact > 0:
		if spent >= caps.exact {
			return 0, true
		}
		effective = caps.exact - spent
	case caps.provider > 0:
		provSpent := proxy.BodyProviderAttemptsSpent(body, provider)
		if provSpent >= caps.provider {
			return 0, true
		}
		effective = caps.provider - provSpent
	}
	if effective < 0 {
		effective = 0
	}
	return effective, false
}

// RecordBodyAttemptsForTest exposes the attempt accounting to package tests.
func RecordBodyAttemptsForTest(body []byte, n int) {
	for i := 0; i < n; i++ {
		proxy.RecordBodyAttempts(body, "deepseek", 1)
	}
}
