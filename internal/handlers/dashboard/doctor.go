// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package dashboard

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	json "encoding/json/v2"

	"alyysha/proxy/internal/auth"
	"alyysha/proxy/internal/backup"
	"alyysha/proxy/internal/handlerutil"
)

// Doctor (N13): one endpoint that answers "is this install healthy" with
// named, graded checks instead of making the operator poke five pages to
// find out. Every check is cheap and local — the doctor is safe to poll from
// the first-run tour, so nothing here dials an upstream provider.

// doctorCheck is one graded probe result.
type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass" | "warn" | "fail"
	Detail  string `json:"detail,omitempty"`
	TookMs  int64  `json:"tookMs,omitempty"`
	FixHint string `json:"fixHint,omitempty"`
}

func (h *DashboardHandler) HandleGetDoctor(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	checks := make([]doctorCheck, 0, 8)
	add := func(name, status, detail, hint string, started time.Time) {
		checks = append(checks, doctorCheck{
			Name: name, Status: status, Detail: detail, FixHint: hint,
			TookMs: time.Since(started).Milliseconds(),
		})
	}

	// 1. The store answers and is structurally sound.
	started := time.Now()
	if raw := h.Repo.RawDB(); raw == nil {
		add("database", "fail", "no database handle", "restart the gateway", started)
	} else if err := raw.PingContext(ctx); err != nil {
		add("database", "fail", "ping failed: "+err.Error(), "check the disk and restart", started)
	} else {
		var ok int
		_ = raw.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&ok)
		_ = ok
		add("database", "pass", "reachable (WAL mode)", "", started)
	}

	// 2. The operator can actually get in: login armed needs a password set.
	started = time.Now()
	if auth.RequireLogin(h.Repo) {
		add("login", "pass", "login required, password set", "", started)
	} else {
		add("login", "warn", "login is disabled — anyone reaching this dashboard manages the gateway",
			"enable Require login in Profile if the dashboard is reachable beyond loopback", started)
	}

	// 3. At least one live connection to route through.
	started = time.Now()
	conns, err := h.Repo.GetProviderConnections("", false)
	if err != nil {
		add("connections", "fail", "read failed: "+err.Error(), "", started)
	} else {
		active := 0
		for _, c := range conns {
			if c.IsActive == 1 {
				active++
			}
		}
		if active == 0 {
			add("connections", "warn", "no active provider connections",
				"add one in Connections before sending traffic", started)
		} else {
			add("connections", "pass", itoa(active)+" active", "", started)
		}
	}

	// 4. At least one usable client key.
	started = time.Now()
	keys, err := h.Repo.GetApiKeys()
	if err != nil {
		add("keys", "fail", "read failed: "+err.Error(), "", started)
	} else {
		active := 0
		for _, k := range keys {
			if k.IsActive == 1 {
				active++
			}
		}
		if active == 0 {
			add("keys", "warn", "no active API keys",
				"create one in API Keys before clients can call", started)
		} else {
			add("keys", "pass", itoa(active)+" active", "", started)
		}
	}

	// 5. The backup destination is writable, so a schedule could actually
	// write there.
	started = time.Now()
	cfg, _ := h.Repo.GetAutoBackupConfig(ctx)
	bm := backup.NewManager(h.Repo)
	dir := bm.ResolveBackupDir(cfg.Destination)
	probe := filepath.Join(dir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0600); err != nil {
		add("backups", "warn", "backup dir "+dir+" is not writable: "+err.Error(),
			"point the backup destination somewhere writable in Profile", started)
	} else {
		_ = os.Remove(probe)
		add("backups", "pass", dir+" writable", "", started)
	}

	overall := "pass"
	for _, c := range checks {
		if c.Status == "fail" {
			overall = "fail"
			break
		}
		if c.Status == "warn" {
			overall = "warn"
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"status":    overall,
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
		"checks":    checks,
	})
}

// itoa is fmt.Sprintf for one int without dragging fmt into the hot path of
// a handler this cheap anyway — kept for symmetry with the check strings.
func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
