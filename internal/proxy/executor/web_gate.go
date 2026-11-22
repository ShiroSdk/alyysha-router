// Alyysha Router — source port and modifications by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package executor

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"alyysha/proxy/internal/handlerutil"
)

var webExecutorsGloballyEnabled atomic.Bool

// SetWebExecutorsEnabled controls the runtime master switch for web executors.
func SetWebExecutorsEnabled(v bool) {
	webExecutorsGloballyEnabled.Store(v)
}

// WebExecutorsEnabled reports whether grey-zone web session executors are permitted to run.
// Default is false (OFF). Set ENABLE_WEB_EXECUTORS=true or enableWebExecutors in settings to enable.
func WebExecutorsEnabled() bool {
	if webExecutorsGloballyEnabled.Load() {
		return true
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_WEB_EXECUTORS")))
	return env == "true" || env == "1"
}

// CheckWebExecutorAllowed checks if web executors are permitted, writing an HTTP 403 error if blocked.
func CheckWebExecutorAllowed(w http.ResponseWriter, provider string) bool {
	if !WebExecutorsEnabled() {
		handlerutil.WriteJSONErrorCode(w, http.StatusForbidden, "web_executors_disabled",
			fmt.Sprintf("%s web session executor is disabled by policy (set ENABLE_WEB_EXECUTORS=true to enable)", provider))
		return false
	}
	return true
}

var claudeWebEnabled atomic.Bool

// SetClaudeWebEnabled controls the claude-web master switch from the stored
// settings and the config. The env var below overrides both directions.
func SetClaudeWebEnabled(v bool) {
	claudeWebEnabled.Store(v)
}

// ClaudeWebEnabled reports whether the unofficial claude.ai lane may run.
// Default is false (OFF): it violates Anthropic's terms of service and the
// endpoints are unverified. CLAUDE_WEB_ENABLED=true forces it on regardless
// of the stored setting, CLAUDE_WEB_ENABLED=false is the operator kill-switch
// that wins over the toggle.
func ClaudeWebEnabled() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDE_WEB_ENABLED")))
	switch env {
	case "true", "1":
		return true
	case "false", "0":
		return false
	}
	return claudeWebEnabled.Load()
}

// CheckClaudeWebAllowed checks if the claude-web lane is permitted, writing
// an HTTP 403 error if blocked.
func CheckClaudeWebAllowed(w http.ResponseWriter, provider string) bool {
	if !ClaudeWebEnabled() {
		handlerutil.WriteJSONErrorCode(w, http.StatusForbidden, "claude_web_disabled",
			fmt.Sprintf("%s is disabled by policy (unofficial provider; set CLAUDE_WEB_ENABLED=true to enable)", provider))
		return false
	}
	return true
}
