// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"alyysha/proxy/internal/models"
)

// comboColumnsLegacy is the shared column list for a database the additive
// pass never reached: every combo reads as unbounded.
const comboColumnsLegacy = `id, name, kind, models, createdAt, updatedAt`

// comboColumnsFull adds the N3 spending-ceiling columns.
const comboColumnsFull = `id, name, kind, models, createdAt, updatedAt, budgetLimitMicros, budgetPeriod`

// defaultComboStrategy is what a combo gets when neither a per-combo override
// nor the global setting names one.
const defaultComboStrategy = "fallback"

// scanCombo fills a combo from a row shaped like the column list the read
// selected: withBudget must match the list, or the scan's column count is
// wrong and every read fails.
func scanCombo(scan func(dest ...any) error, combo *models.Combo, withBudget bool) error {
	if !withBudget {
		return scan(&combo.ID, &combo.Name, &combo.Kind, &combo.Models, &combo.CreatedAt, &combo.UpdatedAt)
	}
	return scan(&combo.ID, &combo.Name, &combo.Kind, &combo.Models, &combo.CreatedAt, &combo.UpdatedAt,
		&combo.BudgetLimitMicros, &combo.BudgetPeriod)
}

// combosBudgetPresent reports (once) whether the combos table carries the N3
// budget columns. Production EnsureAdditiveColumns guarantees them; a
// hand-built fixture without them reads every combo as unbounded instead of
// failing the read.
func (r *Repo) combosBudgetPresent() bool {
	if r == nil || r.db == nil {
		return false
	}
	r.combosBudgetOnce.Do(func() {
		rows, err := r.db.QueryContext(context.Background(), `PRAGMA table_info(combos)`)
		if err != nil {
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notNull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
				return
			}
			if name == "budgetLimitMicros" {
				r.combosBudgetCols = true
				return
			}
		}
	})
	return r.combosBudgetCols
}

// comboColumnList is the projection the combos reads select from.
func (r *Repo) comboColumnList() string {
	if r.combosBudgetPresent() {
		return comboColumnsFull
	}
	return comboColumnsLegacy
}

// resolveComboStrategy picks the strategy for one combo: a per-combo override
// wins, then the global default, then "fallback". A nil settings read (no
// settings row yet) leaves the fallback in place rather than failing the read.
func resolveComboStrategy(combo *models.Combo, s *SettingsData) {
	combo.Strategy = defaultComboStrategy
	if s == nil {
		return
	}
	if cs, ok := s.ComboStrategies[combo.Name]; ok && cs.Strategy != "" {
		combo.Strategy = cs.Strategy
		return
	}
	if s.ComboStrategy != "" {
		combo.Strategy = s.ComboStrategy
	}
}

// GetComboByName retrieves a combo configuration by its name.
func (r *Repo) GetComboByName(name string) (*models.Combo, error) {
	var combo models.Combo
	err := scanCombo(r.db.QueryRow(
		"SELECT "+r.comboColumnList()+" FROM combos WHERE name = ? LIMIT 1",
		name,
	).Scan, &combo, r.combosBudgetPresent())

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	settings, _ := r.GetSettings()
	resolveComboStrategy(&combo, settings)
	return &combo, nil
}

// GetComboById retrieves a combo configuration by its ID.
func (r *Repo) GetComboById(id string) (*models.Combo, error) {
	var combo models.Combo
	err := scanCombo(r.db.QueryRow(
		"SELECT "+r.comboColumnList()+" FROM combos WHERE id = ? LIMIT 1",
		id,
	).Scan, &combo, r.combosBudgetPresent())

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	settings, _ := r.GetSettings()
	resolveComboStrategy(&combo, settings)
	return &combo, nil
}

// GetCombos retrieves all combos from the database.
func (r *Repo) GetCombos() ([]*models.Combo, error) {
	rows, err := r.db.Query("SELECT " + r.comboColumnList() + " FROM combos ORDER BY createdAt ASC")
	if err != nil {
		return nil, err
	}
	// Drain and close the rows BEFORE reading settings: holding an open rows set
	// occupies one pooled connection (MaxOpenConns is 4), so a nested query
	// here could deadlock the whole pool once four concurrent callers pile up.
	var combos []*models.Combo
	for rows.Next() {
		var combo models.Combo
		if err := scanCombo(rows.Scan, &combo, r.combosBudgetPresent()); err != nil {
			rows.Close()
			return nil, err
		}
		combos = append(combos, &combo)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	settings, _ := r.GetSettings()

	// Strategy resolution runs after the rows are closed so the nested settings
	// read can never hold two pooled connections at once.
	for _, combo := range combos {
		resolveComboStrategy(combo, settings)
	}
	return combos, nil
}

// SetComboBudget writes the N3 spending ceiling for one combo. A
// non-positive limit disarms the budget and clears the period; the period is
// stored as sent so the enforcement read resolves the same window the
// operator configured (an unknown spelling reads as monthly, matching the
// budget package's default).
func (r *Repo) SetComboBudget(id string, limitMicros int64, period string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("set combo budget: nil repo")
	}
	if !r.combosBudgetPresent() {
		return fmt.Errorf("set combo budget %s: budget columns missing (run the schema ensure pass)", id)
	}
	if limitMicros <= 0 {
		limitMicros, period = 0, ""
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := r.db.Exec(
		`UPDATE combos SET budgetLimitMicros = ?, budgetPeriod = ?, updatedAt = ? WHERE id = ?`,
		limitMicros, period, now, id,
	); err != nil {
		return fmt.Errorf("set combo budget %s: %w", id, err)
	}
	return nil
}

