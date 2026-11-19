// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package browserlogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"9routerx/proxy/internal/log"
)

// Generic web-login runner (weblogin_run.go): one flow per configured
// provider — open a throwaway browser on the config's page, poll the page
// through CDP until every required token source exists, and answer with the
// captured session. The dedicated gemini and deepseek flows keep their own
// implementations; this runner serves the providers whose whole credential
// is a cookie set (and localStorage keys, when the config names them).

// WebLoginResult is the captured session: the cookie header for the whole
// configured domain, the individual token sources by name, and the
// browser's user agent (the endpoints fingerprint it).
type WebLoginResult struct {
	CookieHeader string
	Tokens       map[string]string
	UserAgent    string
}

// webLoginPollInterval paces the CDP polling.
const webLoginPollInterval = 1200 * time.Millisecond

// webLoginDefaultTimeout bounds one run when the caller passes none.
const webLoginDefaultTimeout = 4 * time.Minute

// RunProvider runs the generic web-login flow for one configured provider.
func RunProvider(ctx context.Context, providerID string) (*WebLoginResult, error) {
	cfg, ok := ConfigFor(providerID)
	if !ok {
		return nil, fmt.Errorf("no web-login config for provider %q", providerID)
	}
	binary := FindBrowser()
	if binary == "" {
		return nil, ErrNoBrowser
	}
	displayEnv := resolveDisplayEnv()
	if runtime.GOOS == "linux" && !envNamesDisplay(displayEnv) {
		// D: a displayless Linux host can still run a web-login when the
		// provider's profile is persistent — a warm session needs no human
		// typing. A provider the operator must sign in to interactively
		// (gemini-web) stays refused: an invisible browser cannot be signed
		// in to.
		if !cfg.PersistentProfile {
			return nil, errors.New("this machine has no display, so the browser cannot be opened; " +
				"use an Incognito window instead and paste the credential manually")
		}
		display, stop, derr := ensureVirtualDisplay(ctx)
		if derr != nil {
			return nil, derr
		}
		if display != "" {
			displayEnv = displayEnvWithVirtual(displayEnv, display)
			defer stop()
		}
	}

	// Profile discipline (C): persistent-profile providers keep their
	// directory on disk (warm session, warm cf_clearance) and are locked
	// per profile so two launches cannot race Chromium's own locks;
	// everything else keeps the throwaway temp profile.
	var profile string
	if cfg.PersistentProfile {
		persistent, err := persistentProfileDir(cfg.ProviderID)
		if err != nil {
			return nil, err
		}
		unlock, ok := lockProfile(persistent)
		if !ok {
			return nil, fmt.Errorf("a web-login for %s is already running; try again when it finishes", cfg.ProviderID)
		}
		profile = persistent
		defer unlock()
	} else {
		temp, err := os.MkdirTemp(browserProfileRoot(binary), "9router-"+cfg.ProviderID+"-login-*")
		if err != nil {
			return nil, fmt.Errorf("create the temporary browser profile: %w", err)
		}
		profile = temp
		// The profile holds a signed-in session. Leaving it on disk would
		// leave the credential in a temporary directory nothing cleans up.
		defer func() { _ = os.RemoveAll(profile) }()
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocate a debugging port: %w", err)
	}

	command := exec.CommandContext(ctx, binary,
		fmt.Sprintf("--user-data-dir=%s", profile),
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-allow-origins=*",
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		cfg.LoginURL,
	)
	command.Env = displayEnv
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("open the browser: %w", err)
	}
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	}()

	exited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(exited)
	}()

	log.Info(cfg.ProviderID, "web-login browser opened", "url", cfg.LoginURL)

	timeout := webLoginDefaultTimeout
	if deadline, has := ctx.Deadline(); has {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	ticker := time.NewTicker(webLoginPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			return nil, ErrNoSignIn
		case <-ticker.C:
			result, ok := readWebSession(ctx, port, cfg)
			if ok {
				log.Info(cfg.ProviderID, "web-login captured the session")
				return result, nil
			}
			if timeout <= 0 {
				return nil, ErrNoSignIn
			}
			timeout -= webLoginPollInterval
		}
	}
}

// readWebSession reads one poll of the browser state: every required token
// source must exist for the capture to count as a sign-in.
func readWebSession(ctx context.Context, port int, cfg WebLoginConfig) (*WebLoginResult, bool) {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	userAgent, webSocketURL := browserEndpoint(callCtx, port, siteURLForConfig(cfg))
	if webSocketURL == "" {
		return nil, false
	}

	seen := map[string]string{}
	var headerParts []string
	if cookies := cdpAllCookies(callCtx, webSocketURL); cookies != nil {
		for _, c := range cookies {
			if cfg.CookieDomain != "" && !cookieDomainMatches(c.Domain, cfg.CookieDomain) {
				continue
			}
			if c.Name == "" || c.Value == "" {
				continue
			}
			if _, dup := seen[c.Name]; dup {
				continue
			}
			seen[c.Name] = c.Value
			headerParts = append(headerParts, c.Name+"="+c.Value)
		}
	}

	tokens := map[string]string{}
	for _, src := range cfg.TokenSources {
		switch src.Kind {
		case TokenSourceCookie:
			value := seen[src.Name]
			if value == "" {
				return nil, false
			}
			tokens[src.Name] = value
		case TokenSourceLocalStorage:
			value, ok := cdpLocalStorageGet(callCtx, webSocketURL, src.Name)
			if !ok || value == "" {
				return nil, false
			}
			tokens[src.Name] = value
		}
	}
	return &WebLoginResult{
		CookieHeader: strings.Join(headerParts, "; "),
		Tokens:       tokens,
		UserAgent:    userAgent,
	}, true
}

// siteURLForConfig is the site prefix the page list is narrowed by.
func siteURLForConfig(cfg WebLoginConfig) string {
	return strings.TrimSuffix(cfg.LoginURL, "/")
}

// browserCookie is one cookie as the CDP call returns it.
type browserCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

// cdpAllCookies reads the browser's whole cookie jar through CDP. Nil on
// any failure — the caller treats that as "not signed in yet".
func cdpAllCookies(ctx context.Context, webSocketURL string) []browserCookie {
	payload, err := json.Marshal(map[string]any{
		"id":     1,
		"method": "Network.getAllCookies",
	})
	if err != nil {
		return nil
	}
	reply, err := cdpCall(ctx, webSocketURL, string(payload))
	if err != nil {
		return nil
	}
	var payloadOut struct {
		Cookies []browserCookie `json:"cookies"`
	}
	if err := json.Unmarshal(reply, &payloadOut); err != nil {
		return nil
	}
	return payloadOut.Cookies
}

// cdpLocalStorageGet reads one localStorage key off the page the websocket
// points at. The expression is JS SOURCE (Runtime.evaluate evaluates what
// it is given as JavaScript), mirroring the deepseek reader.
func cdpLocalStorageGet(ctx context.Context, webSocketURL, key string) (string, bool) {
	expression := fmt.Sprintf("localStorage.getItem(%q)", key)
	payload, err := json.Marshal(map[string]any{
		"id":     2,
		"method": "Runtime.evaluate",
		"params": map[string]any{"expression": expression, "returnByValue": true},
	})
	if err != nil {
		return "", false
	}
	reply, err := cdpCall(ctx, webSocketURL, string(payload))
	if err != nil {
		return "", false
	}
	var evaluated struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(reply, &evaluated); err != nil {
		return "", false
	}
	if len(evaluated.Value) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(evaluated.Value, &value); err != nil {
		// A non-string value (an object, a number) is not a credential
		// spelling this flow can hand back; the raw form is the caller's
		// business, and the config's validator sees it as it is.
		value = string(evaluated.Value)
	}
	return value, true
}

// cookieDomainMatches reports whether a cookie's own domain belongs to the
// configured domain: normalized (trimmed, leading dots stripped, lowercased)
// then matched exact or as a deeper subdomain. A plain substring match is
// forbidden — attacker.tld must never satisfy .example.com.
func cookieDomainMatches(actual, expected string) bool {
	normalize := func(d string) string {
		d = strings.TrimSpace(d)
		d = strings.TrimPrefix(d, ".")
		return strings.ToLower(d)
	}
	a, e := normalize(actual), normalize(expected)
	if e == "" || a == "" {
		return false
	}
	return a == e || strings.HasSuffix(a, "."+e)
}
