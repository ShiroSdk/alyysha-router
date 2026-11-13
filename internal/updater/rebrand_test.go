// Alyysha Router — built by YannClipper — https://t.me/YannClippers
package updater

import (
	"strings"
	"testing"
)

// TestRebrandUpdateChannelPointsAtAlyysha Router pins the R.1 rebrand: the default
// update channel must answer from the Alyysha Router repository, never from the
// upstream fork's. A build that still points at luqman-v1/alyysha would
// offer every user the ORIGINAL author's binary on `alyysha update` —
// silently overwriting this codebase with someone else's release.
func TestRebrandUpdateChannelPointsAtAlyysha Router(t *testing.T) {
	if !strings.Contains(DefaultUpdateURL, "ShiroSdk/alyysha-router") {
		t.Errorf("DefaultUpdateURL = %q, must point at ShiroSdk/alyysha-router", DefaultUpdateURL)
	}
	if strings.Contains(DefaultUpdateURL, "luqman-v1") || strings.Contains(DefaultUpdateURL, "alyysha"+"-go") {
		t.Errorf("DefaultUpdateURL still references the old repo: %q", DefaultUpdateURL)
	}
	if DefaultGitHubRepo != "ShiroSdk/alyysha-router" {
		t.Errorf("DefaultGitHubRepo = %q, want ShiroSdk/alyysha-router", DefaultGitHubRepo)
	}
}
