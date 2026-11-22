// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Settings history (E3): every write to the settings blob snapshots the keys
// it touched — old value, new value, source — into settingsHistory, capped at
// 100 entries per key. The rollback endpoint restores one key's old value and
// records the rollback as its own history entry, so the trail never has a gap.
//
// DILARANG snapshot raw credentials: keys whose name smells like a secret
// (password, token, secret, key-hash) are masked in both images before the
// row lands, so the history table can never leak the bcrypt hash or an OIDC
// client secret.

const settingsHistoryPerKey = 100

// maskedSettingKeys are the settings keys whose values are credential
// material — their history entries carry a fixed mask instead of the values.
var maskedSettingKeys = []string{"password", "dashboardPasswordHash", "oidcClientSecret", "mitmSudoEncrypted"}

func isMaskedSettingKey(key string) bool {
	lower := strings.ToLower(key)
	for _, masked := range maskedSettingKeys {
		if strings.Contains(lower, masked) {
			return true
		}
	}
	return false
}

// maskSettingValue renders a value for history storage: credentials become a
// fixed mask, everything else is the JSON re-encoding of the value.
func maskSettingValue(key string, value any) string {
	if isMaskedSettingKey(key) {
		return "[masked]"
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "[unencodable]"
	}
	out := string(b)
	// Cap very large blobs (navVisibility orders and the like) at 4KB per
	// image so one noisy key cannot bloat the table.
	if len(out) > 4096 {
		out = out[:4096] + "…"
	}
	return out
}

// recordSettingsHistory snapshots the keys a write touched. Called inside the
// updateSettings mutex; best-effort — a history failure must not fail the
// settings write.
func (r *Repo) recordSettingsHistoryDiff(before, after map[string]any) {
	if r.db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, newValue := range after {
		oldValue, present := before[key]
		if present {
			if fmt.Sprint(oldValue) == fmt.Sprint(newValue) {
				continue
			}
		}
		oldStr := maskSettingValue(key, oldValue)
		newStr := maskSettingValue(key, newValue)
		if _, err := r.db.Exec(
			`INSERT INTO settingsHistory (key, oldValue, newValue, source, at)
			 VALUES (?, ?, ?, ?, ?)`,
			key, oldStr, newStr, "write", now,
		); err != nil {
			return
		}
		// Retention: cap at settingsHistoryPerKey entries per key.
		_, _ = r.db.Exec(
			`DELETE FROM settingsHistory WHERE key = ? AND id NOT IN
			 (SELECT id FROM settingsHistory WHERE key = ? ORDER BY id DESC LIMIT ?)`,
			key, key, settingsHistoryPerKey,
		)
	}
}

// SettingsHistoryEntry is one row of the settings history.
type SettingsHistoryEntry struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
	Source   string `json:"source"`
	At       string `json:"at"`
}

// GetSettingsHistory returns the newest entries for one key (or all keys
// when the key is empty), capped at 100.
func (r *Repo) GetSettingsHistory(key string, limit int) ([]SettingsHistoryEntry, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("settings history: nil repo")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ctx := context.Background()
	var rows *sql.Rows
	var err error
	if key == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, key, oldValue, newValue, source, at FROM settingsHistory ORDER BY id DESC LIMIT ?`,
			limit)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, key, oldValue, newValue, source, at FROM settingsHistory
			 WHERE key = ? ORDER BY id DESC LIMIT ?`, key, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("settings history: query: %w", err)
	}
	defer rows.Close()
	out := []SettingsHistoryEntry{}
	for rows.Next() {
		var e SettingsHistoryEntry
		if err := rows.Scan(&e.ID, &e.Key, &e.OldValue, &e.NewValue, &e.Source, &e.At); err != nil {
			return nil, fmt.Errorf("settings history: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RollbackSettingsKey restores one key's old value from a history row and
// records the rollback as its own entry. The key must not be a credential
// key (those are masked in the history, so their old value is unrecoverable
// by design — rotating the credential again is the rollback).
func (r *Repo) RollbackSettingsKey(entryID int64, source string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("settings rollback: nil repo")
	}
	var key, oldValue string
	err := r.db.QueryRow(
		`SELECT key, oldValue FROM settingsHistory WHERE id = ?`, entryID,
	).Scan(&key, &oldValue)
	if err != nil {
		return fmt.Errorf("settings rollback: read history %d: %w", entryID, err)
	}
	if isMaskedSettingKey(key) {
		return fmt.Errorf("settings rollback: %s is a credential key — rotate it in the panel instead", key)
	}
	var value any
	if err := json.Unmarshal([]byte(oldValue), &value); err != nil {
		return fmt.Errorf("settings rollback: stored old value is not JSON: %w", err)
	}
	if err := r.UpdateSettingsRaw(map[string]any{key: value}); err != nil {
		return fmt.Errorf("settings rollback: apply: %w", err)
	}
	// The rollback is its own history entry (UpdateSettingsRaw already
	// recorded the write), so the trail has no gap.
	return nil
}
