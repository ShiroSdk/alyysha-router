// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"9routerx/proxy/internal/keikey"
	"9routerx/proxy/internal/models"
)

// Operator-minted dashboard access tokens (N5 RBAC). Each token carries one
// scope — admin, write or read — and the middleware enforces it per route and
// method, failing closed: a presented token that is unknown, revoked or out
// of scope never degrades to the anonymous path.
//
// The credential is stored hashed with the same primitives the engine keys
// use: a fast lookup hash for the row scan and an Argon2id verifier for the
// check, so a database dump does not hand out usable dashboard credentials.
// The plaintext is shown exactly once, in the mint response.

// DashboardTokenScopes are the scopes a token can carry, in decreasing
// privilege order. admin is the dashboard session's equal; write covers the
// management API but not the destructive or secret-material surfaces; read is
// GET-only observability.
const (
	DashboardTokenScopeAdmin = "admin"
	DashboardTokenScopeWrite = "write"
	DashboardTokenScopeRead  = "read"
)

// IsValidDashboardTokenScope reports whether the spelling names a scope.
func IsValidDashboardTokenScope(scope string) bool {
	switch scope {
	case DashboardTokenScopeAdmin, DashboardTokenScopeWrite, DashboardTokenScopeRead:
		return true
	}
	return false
}

// CreateDashboardToken stores a minted token. The plaintext never reaches
// this layer twice: the caller hashes it before calling.
func (r *Repo) CreateDashboardToken(ctx context.Context, id, name, tokenHash, lookupHash, scope string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("create dashboard token: nil repo")
	}
	if !IsValidDashboardTokenScope(scope) {
		return fmt.Errorf("create dashboard token: invalid scope %q", scope)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO dashboardTokens (id, name, lookupHash, tokenHash, scope, createdAt) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, lookupHash, tokenHash, scope, now,
	)
	if err != nil {
		return fmt.Errorf("create dashboard token: %w", err)
	}
	return nil
}

// ListDashboardTokens projects the public fields of every token, newest
// first. The hashes are deliberately not selected: a list response has no
// use for them and a leak surface it does not need.
func (r *Repo) ListDashboardTokens(ctx context.Context) ([]*models.DashboardToken, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list dashboard tokens: nil repo")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, scope, COALESCE(createdAt, ''), COALESCE(lastUsedAt, ''), COALESCE(revokedAt, '')
		 FROM dashboardTokens ORDER BY createdAt DESC`)
	if err != nil {
		return nil, fmt.Errorf("list dashboard tokens: %w", err)
	}
	defer rows.Close()
	out := []*models.DashboardToken{}
	for rows.Next() {
		t := &models.DashboardToken{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Scope, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("list dashboard tokens: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeDashboardToken stamps the revocation instant. The row stays: an
// audit trail of what existed beats a hole where it was, and resolution
// refuses revoked tokens anyway.
func (r *Repo) RevokeDashboardToken(ctx context.Context, id string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("revoke dashboard token: nil repo")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.ExecContext(ctx,
		`UPDATE dashboardTokens SET revokedAt = ? WHERE id = ? AND revokedAt = ''`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("revoke dashboard token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("revoke dashboard token: token %s not found or already revoked", id)
	}
	return nil
}

// ResolveDashboardToken verifies a presented plaintext and returns the token
// row when it is live: found, matching, and not revoked. An empty plaintext
// resolves to nothing rather than to every token.
func (r *Repo) ResolveDashboardToken(ctx context.Context, plaintext string) (*models.DashboardToken, error) {
	if r == nil || r.db == nil || plaintext == "" {
		return nil, nil
	}
	lookup := keikey.LookupHash(plaintext)
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, scope, COALESCE(createdAt, ''), COALESCE(revokedAt, ''), tokenHash FROM dashboardTokens WHERE lookupHash = ?`,
		lookup,
	)
	var id, name, scope, createdAt, revokedAt, tokenHash string
	if err := row.Scan(&id, &name, &scope, &createdAt, &revokedAt, &tokenHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve dashboard token: %w", err)
	}
	if revokedAt != "" || !keikey.Verify(plaintext, tokenHash) {
		return nil, nil
	}
	// Best-effort last-used stamp: a failed touch must not fail the request
	// it authenticated.
	_, _ = r.db.ExecContext(ctx, `UPDATE dashboardTokens SET lastUsedAt = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return &models.DashboardToken{
		ID:        id,
		Name:      name,
		Scope:     scope,
		CreatedAt: createdAt,
	}, nil
}
