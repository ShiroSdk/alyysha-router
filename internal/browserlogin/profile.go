// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package browserlogin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"alyysha/proxy/internal/config"
)

// Persistent browser profiles (profile.go): a configured provider's
// throwaway browser keeps its profile on disk under
// config.ResolveDataDir()/browser-profiles/<provider>/ so the second login
// is fast (the session is warm) and edge cookies such as cf_clearance
// survive. The directory carries credentials — it is created 0700, never
// exported, and never included in any backup (the backup snapshots the
// SQLite database only; see internal/backup/backup.go).
//
// Gemini stays on a throwaway profile by design: Google rotates
// __Secure-1PSIDTS while a browser is open, and a lingering profile cancels
// the router's session out — see the webLoginConfigs comment.

// profileLocks serializes launches per profile directory: two concurrent
// runs against one profile would race Chromium's own locks and both fail.
var profileLocks sync.Map

// lockProfile acquires the per-profile launch lock. A false return means
// another run already holds it.
func lockProfile(dir string) (func(), bool) {
	loaded, _ := profileLocks.LoadOrStore(dir, &sync.Mutex{})
	mu := loaded.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

// ProfileRoot is the directory every persistent browser profile lives in.
func ProfileRoot() string {
	return filepath.Join(config.ResolveDataDir(), "browser-profiles")
}

// persistentProfileDir resolves (and creates, 0700) one provider's
// persistent profile directory.
func persistentProfileDir(providerID string) (string, error) {
	dir := filepath.Join(config.ResolveDataDir(), "browser-profiles", providerID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create the persistent profile directory: %w", err)
	}
	return dir, nil
}

// ResetProfile deletes one provider's persistent profile directory — the
// operator's "reset profile" action. It returns the removed path.
func ResetProfile(providerID string) (string, error) {
	cfg, ok := webLoginConfigs[providerID]
	if !ok {
		return "", fmt.Errorf("no web-login config for provider %q", providerID)
	}
	if !cfg.PersistentProfile {
		return "", fmt.Errorf("provider %q keeps no persistent profile", providerID)
	}
	dir := filepath.Join(config.ResolveDataDir(), "browser-profiles", providerID)
	unlock, ok := lockProfile(dir)
	if !ok {
		return "", fmt.Errorf("a web-login for %s is already running; try again when it finishes", providerID)
	}
	defer unlock()
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("remove the profile directory: %w", err)
	}
	return dir, nil
}
