// 9RouterX — ported & modified by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	json "encoding/json/v2"

	"9routerx/proxy/internal/backup"
	"9routerx/proxy/internal/db"
	"9routerx/proxy/internal/handlerutil"
)

// HandleGetAutoBackup handles GET /api/settings/auto-backup.
func (h *DashboardHandler) HandleGetAutoBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := h.Repo.GetAutoBackupConfig(ctx)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, err := h.Repo.GetAutoBackupStatus(ctx)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	bm := backup.NewManager(h.Repo)
	items, _ := bm.ListBackups(cfg.Destination)

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"config":  sanitizeAutoBackupConfig(cfg),
		"status":  status,
		"backups": items,
	})
}

// HandlePatchAutoBackup handles PATCH /api/settings/auto-backup.
func (h *DashboardHandler) HandlePatchAutoBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var patch struct {
		Enabled       *bool   `json:"enabled"`
		IntervalHours *int    `json:"intervalHours"`
		Retention     *int    `json:"retention"`
		Destination   *string `json:"destination"`
		Remote        *struct {
			Kind          string  `json:"kind"`
			TelegramToken *string `json:"telegramToken"`
			TelegramChat  *string `json:"telegramChat"`
			GithubToken   *string `json:"githubToken"`
			GithubRepo    *string `json:"githubRepo"`
			GithubBranch  *string `json:"githubBranch"`
			GithubPath    *string `json:"githubPath"`
		} `json:"remote"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &patch); err != nil {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}

	cfg, err := h.Repo.GetAutoBackupConfig(ctx)
	if err != nil {
		cfg = db.DefaultAutoBackupConfig()
	}

	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.IntervalHours != nil && *patch.IntervalHours > 0 {
		cfg.IntervalHours = *patch.IntervalHours
	}
	if patch.Retention != nil && *patch.Retention > 0 {
		cfg.Retention = *patch.Retention
	}
	if patch.Destination != nil {
		cfg.Destination = strings.TrimSpace(*patch.Destination)
	}
	// N8: the remote block merges — credential fields the caller omitted
	// keep their stored values, so a UI toggle for the kind does not wipe
	// the bot token the operator pasted yesterday.
	if patch.Remote != nil {
		r := patch.Remote
		switch strings.ToLower(strings.TrimSpace(r.Kind)) {
		case "", db.RemoteKindTelegram, db.RemoteKindGithub:
			cfg.Remote.Kind = strings.ToLower(strings.TrimSpace(r.Kind))
		default:
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "remote.kind must be empty, telegram or github")
			return
		}
		if r.TelegramToken != nil {
			cfg.Remote.TelegramToken = strings.TrimSpace(*r.TelegramToken)
		}
		if r.TelegramChat != nil {
			cfg.Remote.TelegramChat = strings.TrimSpace(*r.TelegramChat)
		}
		if r.GithubToken != nil {
			cfg.Remote.GithubToken = strings.TrimSpace(*r.GithubToken)
		}
		if r.GithubRepo != nil {
			cfg.Remote.GithubRepo = strings.TrimSpace(*r.GithubRepo)
		}
		if r.GithubBranch != nil {
			cfg.Remote.GithubBranch = strings.TrimSpace(*r.GithubBranch)
		}
		if r.GithubPath != nil {
			cfg.Remote.GithubPath = strings.TrimSpace(*r.GithubPath)
		}
		if cfg.Remote.Kind != "" && !cfg.RemoteArmed() {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "remote credentials incomplete for kind "+cfg.Remote.Kind)
			return
		}
	}

	if err := h.Repo.SetAutoBackupConfig(ctx, cfg); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status, _ := h.Repo.GetAutoBackupStatus(ctx)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"config": sanitizeAutoBackupConfig(cfg),
		"status": status,
	})
}

// sanitizeAutoBackupConfig strips the remote credentials from a config
// bound for a response body: the tokens never travel back out, and the UI
// learns only whether one is stored.
func sanitizeAutoBackupConfig(cfg db.AutoBackupConfig) map[string]any {
	return map[string]any{
		"enabled":       cfg.Enabled,
		"intervalHours": cfg.IntervalHours,
		"retention":     cfg.Retention,
		"destination":   cfg.Destination,
		"remote": map[string]any{
			"kind":             cfg.Remote.Kind,
			"telegramChat":     cfg.Remote.TelegramChat,
			"githubRepo":       cfg.Remote.GithubRepo,
			"githubBranch":     cfg.Remote.GithubBranch,
			"githubPath":       cfg.Remote.GithubPath,
			"hasTelegramToken": cfg.Remote.TelegramToken != "",
			"hasGithubToken":   cfg.Remote.GithubToken != "",
		},
	}
}

// HandlePostAutoBackup handles POST /api/settings/auto-backup (actions: backup, restore, delete).
func (h *DashboardHandler) HandlePostAutoBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var req struct {
		Action string `json:"action"` // "backup", "restore", "delete"
		File   string `json:"file"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}

	bm := backup.NewManager(h.Repo)
	action := strings.ToLower(strings.TrimSpace(req.Action))

	switch action {
	case "backup":
		res, err := bm.CreateBackup(ctx, h.ExportDatabaseJSON)
		if err != nil {
			handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"result": res,
		})

	case "restore":
		file := strings.TrimSpace(req.File)
		if file == "" {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "file parameter is required for restore")
			return
		}
		// Resolve file relative to backup dir if base name only
		if !filepath.IsAbs(file) {
			cfg, _ := h.Repo.GetAutoBackupConfig(ctx)
			file = filepath.Join(bm.ResolveBackupDir(cfg.Destination), file)
		}

		if err := backup.RestoreDatabase(ctx, h.Repo, file, h.ImportDatabaseJSON); err != nil {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, fmt.Sprintf("restore failed: %v", err))
			return
		}
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": filepath.Base(file)})

	case "delete":
		file := strings.TrimSpace(req.File)
		if file == "" {
			handlerutil.WriteJSONError(w, http.StatusBadRequest, "file parameter is required for delete")
			return
		}
		if !filepath.IsAbs(file) {
			cfg, _ := h.Repo.GetAutoBackupConfig(ctx)
			file = filepath.Join(bm.ResolveBackupDir(cfg.Destination), file)
		}
		_ = os.Remove(file)
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": filepath.Base(file)})

	default:
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "unknown action: "+action)
	}
}
