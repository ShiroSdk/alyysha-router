// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package browserlogin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The persistent-profile discipline (C): one 0700 directory per provider
// under the data dir's browser-profiles root, a per-profile launch lock,
// and a reset that wipes the directory.
func TestPersistentProfileDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	dir, err := persistentProfileDir("perplexity-web")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(dataDir, "browser-profiles", "perplexity-web")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Windows has no executable permission bits: the 0700 request is
	// honored on POSIX, and on Windows only the read-only bit exists.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("perm = %v, want 0700", info.Mode().Perm())
	}
	// Recreating is a no-op.
	if _, err := persistentProfileDir("perplexity-web"); err != nil {
		t.Fatalf("re-resolve: %v", err)
	}
}

func TestProfileLockSerializes(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	dir := filepath.Join(dataDir, "browser-profiles", "locked")

	unlock, ok := lockProfile(dir)
	if !ok {
		t.Fatal("first lock refused")
	}
	if _, ok := lockProfile(dir); ok {
		t.Error("second lock acquired while the first is held")
	}
	unlock()
	unlock2, ok := lockProfile(dir)
	if !ok {
		t.Fatal("lock not reacquired after release")
	}
	unlock2()
}

func TestResetProfile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	dir, err := persistentProfileDir("perplexity-web")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	seed := filepath.Join(dir, "Default", "Preferences")
	if err := os.MkdirAll(filepath.Dir(seed), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seed, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := ResetProfile("perplexity-web")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.HasSuffix(removed, "perplexity-web") {
		t.Errorf("removed = %q", removed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("profile survived the reset: %v", err)
	}

	// A provider without a persistent profile refuses the reset.
	if _, err := ResetProfile("gemini-web"); err == nil {
		t.Error("gemini reset accepted — the throwaway discipline must hold")
	}
	// An unknown provider refuses too.
	if _, err := ResetProfile("no-such-provider"); err == nil {
		t.Error("unknown provider reset accepted")
	}
}

// The gemini config keeps the throwaway discipline; the generic flow's
// persistent flag follows the config.
func TestGeminiKeepsThrowawayProfile(t *testing.T) {
	cfg, ok := ConfigFor("gemini-web")
	if !ok {
		t.Fatal("gemini-web config missing")
	}
	if cfg.PersistentProfile {
		t.Error("gemini-web must keep a throwaway profile (Google rotates __Secure-1PSIDTS)")
	}
	for _, provider := range []string{"perplexity-web", "grok-web", "chatgpt-web", "muse-web"} {
		cfg, ok := ConfigFor(provider)
		if !ok {
			t.Fatalf("%s config missing", provider)
		}
		if !cfg.PersistentProfile {
			t.Errorf("%s must keep a persistent profile", provider)
		}
	}
}

// Cookie-domain matching is suffix-based on the normalized host: a plain
// substring never satisfies it.
func TestCookieDomainMatches(t *testing.T) {
	cases := []struct {
		actual, expected string
		want             bool
	}{
		{".perplexity.ai", "perplexity.ai", true},
		{"www.perplexity.ai", "perplexity.ai", true},
		{"perplexity.ai.evil.tld", "perplexity.ai", false},
		{"notperplexity.ai", "perplexity.ai", false},
		{"", "perplexity.ai", false},
	}
	for _, tc := range cases {
		if got := cookieDomainMatches(tc.actual, tc.expected); got != tc.want {
			t.Errorf("cookieDomainMatches(%q, %q) = %v, want %v", tc.actual, tc.expected, got, tc.want)
		}
	}
}
