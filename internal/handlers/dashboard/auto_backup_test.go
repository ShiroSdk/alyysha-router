// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package dashboard

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alyysha/proxy/internal/backup"
	"alyysha/proxy/internal/db"
)

func TestAutoBackupDashboardEndpoints(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	h := NewDashboardHandler(repo, nil)
	backupDir := t.TempDir()

	// Seed config
	_ = repo.SetAutoBackupConfig(t.Context(), db.AutoBackupConfig{
		Enabled:       true,
		IntervalHours: 12,
		Retention:     4,
		Destination:   backupDir,
	})

	t.Run("GET /api/settings/auto-backup returns config, status, and list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/settings/auto-backup", nil)
		rec := httptest.NewRecorder()
		h.HandleGetAutoBackup(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			Config  db.AutoBackupConfig `json:"config"`
			Status  db.AutoBackupStatus `json:"status"`
			Backups []backup.BackupItem `json:"backups"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !resp.Config.Enabled || resp.Config.IntervalHours != 12 || resp.Config.Retention != 4 {
			t.Errorf("unexpected config: %+v", resp.Config)
		}
	})

	t.Run("PATCH /api/settings/auto-backup updates config", func(t *testing.T) {
		patchBody := []byte(`{"enabled":false,"intervalHours":48,"retention":10}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/settings/auto-backup", bytes.NewReader(patchBody))
		rec := httptest.NewRecorder()
		h.HandlePatchAutoBackup(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		cfg, _ := repo.GetAutoBackupConfig(t.Context())
		if cfg.Enabled || cfg.IntervalHours != 48 || cfg.Retention != 10 {
			t.Errorf("config not updated: %+v", cfg)
		}
	})

	t.Run("POST /api/settings/auto-backup action:backup creates snapshots", func(t *testing.T) {
		reqBody := []byte(`{"action":"backup"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/settings/auto-backup", bytes.NewReader(reqBody))
		rec := httptest.NewRecorder()
		h.HandlePostAutoBackup(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Ok     bool                 `json:"ok"`
			Result *backup.BackupResult `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !resp.Ok || resp.Result == nil || resp.Result.SQLitePath == "" {
			t.Fatalf("unexpected backup result: %+v", resp)
		}

		// Now test action:delete on that created file
		delPayload, _ := json.Marshal(map[string]any{
			"action": "delete",
			"file":   resp.Result.SQLitePath,
		})
		delReq := httptest.NewRequest(http.MethodPost, "/api/settings/auto-backup", bytes.NewReader(delPayload))
		delRec := httptest.NewRecorder()
		h.HandlePostAutoBackup(delRec, delReq)

		if delRec.Code != http.StatusOK {
			t.Fatalf("delete status = %d, want 200: %s", delRec.Code, delRec.Body.String())
		}
	})
}

// TestAutoBackupRemoteSanitized proves the credentials never travel back out:
// the response carries the has-token flags, never the tokens themselves, and
// a PATCH that changes only the kind keeps the stored tokens.
func TestAutoBackupRemoteSanitized(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo, nil)
	ctx := t.Context()

	_ = repo.SetAutoBackupConfig(ctx, db.AutoBackupConfig{
		Remote: db.AutoBackupRemote{
			Kind: db.RemoteKindTelegram, TelegramToken: "SECRET-TG", TelegramChat: "42",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/auto-backup", nil)
	rec := httptest.NewRecorder()
	h.HandleGetAutoBackup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "SECRET-TG") {
		t.Error("telegram token leaked in GET response")
	}
	if !strings.Contains(body, `"hasTelegramToken":true`) {
		t.Error("hasTelegramToken flag missing")
	}

	// A PATCH that only changes the kind keeps the stored token, so a UI
	// toggle does not wipe the credential pasted yesterday.
	patch := `{"remote":{"kind":"telegram","telegramChat":"43"}}`
	req = httptest.NewRequest(http.MethodPatch, "/api/settings/auto-backup", strings.NewReader(patch))
	rec = httptest.NewRecorder()
	h.HandlePatchAutoBackup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SECRET-TG") {
		t.Error("telegram token leaked in PATCH response")
	}
	cfg, _ := repo.GetAutoBackupConfig(ctx)
	if cfg.Remote.TelegramToken != "SECRET-TG" {
		t.Errorf("stored token lost on partial patch: %+v", cfg.Remote)
	}
	if cfg.Remote.TelegramChat != "43" {
		t.Errorf("chat not updated: %+v", cfg.Remote)
	}
}
