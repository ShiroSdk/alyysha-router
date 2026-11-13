// Alyysha Router — built by YannClipper — https://t.me/YannClippers
package envcompat

import (
	"os"
	"testing"
)

// TestGetenvNewNameWins pins the R.4 compat contract: the ALYYSHA_ spelling
// is authoritative, the pre-rename spelling answers only when the new one is
// unset, and neither the presence of the old name nor its precedence order
// among the arguments changes which value wins.
func TestGetenvNewNameWins(t *testing.T) {
	t.Setenv("ALYYSHA_URL", "https://new.example")
	t.Setenv("NINEROUTER_URL", "https://old.example")
	if got := Getenv("ALYYSHA_URL", "NINEROUTER_URL"); got != "https://new.example" {
		t.Errorf("Getenv = %q, want the new name to win", got)
	}

	t.Setenv("ALYYSHA_URL", "")
	if got := Getenv("ALYYSHA_URL", "NINEROUTER_URL"); got != "https://old.example" {
		t.Errorf("Getenv = %q, want the legacy fallback to answer", got)
	}

	t.Setenv("NINEROUTER_URL", "")
	if got := Getenv("ALYYSHA_URL", "NINEROUTER_URL"); got != "" {
		t.Errorf("Getenv = %q, want empty when both unset", got)
	}
}

// TestGetenvEmptyNameSkipped covers the conditional list building: an empty
// slot must be skipped, not returned as a found-but-empty value.
func TestGetenvEmptyNameSkipped(t *testing.T) {
	t.Setenv("NINEROUTER_PEER_TOKEN", "tok")
	if got := Getenv("ALYYSHA_PEER_TOKEN", "", "NINEROUTER_PEER_TOKEN"); got != "tok" {
		t.Errorf("Getenv = %q, want legacy value through the empty slot", got)
	}
}

// TestGetenvNoWarningWhenCurrentSet: the deprecation warning exists to move
// operators off the old name; it must stay silent while the new name answers.
func TestGetenvNoWarningWhenCurrentSet(t *testing.T) {
	loggedMu.Lock()
	logged = map[string]bool{}
	loggedMu.Unlock()

	t.Setenv("ALYYSHA_ROUTE_HEADERS", "1")
	t.Setenv("NINEROUTER_ROUTE_HEADERS", "0")
	_ = Getenv("ALYYSHA_ROUTE_HEADERS", "NINEROUTER_ROUTE_HEADERS")

	loggedMu.Lock()
	_, warned := logged["ALYYSHA_ROUTE_HEADERS\x00NINEROUTER_ROUTE_HEADERS"]
	loggedMu.Unlock()
	if warned {
		t.Error("deprecation warning fired while the current name was set")
	}
}

// TestIsDeprecatedSet pins the doctor-check shape: true only for the exact
// "new unset, old set" combination.
func TestIsDeprecatedSet(t *testing.T) {
	os.Unsetenv("ALYYSHA_HEADROOM_LIVE")
	os.Unsetenv("NINEROUTER_HEADROOM_LIVE")
	t.Cleanup(func() {
		os.Unsetenv("ALYYSHA_HEADROOM_LIVE")
		os.Unsetenv("NINEROUTER_HEADROOM_LIVE")
	})

	if IsDeprecatedSet("ALYYSHA_HEADROOM_LIVE", "NINEROUTER_HEADROOM_LIVE") {
		t.Error("deprecated report with both unset")
	}
	t.Setenv("NINEROUTER_HEADROOM_LIVE", "1")
	if !IsDeprecatedSet("ALYYSHA_HEADROOM_LIVE", "NINEROUTER_HEADROOM_LIVE") {
		t.Error("legacy-only set must report deprecated")
	}
	t.Setenv("ALYYSHA_HEADROOM_LIVE", "1")
	if IsDeprecatedSet("ALYYSHA_HEADROOM_LIVE", "NINEROUTER_HEADROOM_LIVE") {
		t.Error("current name set must not report deprecated")
	}
}
