// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package db

import (
	"context"
	"testing"

	"9routerx/proxy/internal/keikey"
)

// The N5 dashboard access token: hashed with the engine-key primitives,
// resolved by plaintext, revoked by stamp.
func TestDashboardTokenLifecycle(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	ctx := context.Background()

	plaintext := "9rdt_test_deadbeef"
	hash, err := keikey.Hash(plaintext)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := repo.CreateDashboardToken(ctx, "dtok_1", "ci", hash, keikey.LookupHash(plaintext), "write"); err != nil {
		t.Fatalf("create: %v", err)
	}

	tok, err := repo.ResolveDashboardToken(ctx, plaintext)
	if err != nil || tok == nil {
		t.Fatalf("resolve: %v (nil=%v)", err, tok == nil)
	}
	if tok.Scope != "write" || tok.Name != "ci" {
		t.Errorf("resolved = %+v", tok)
	}
	// A wrong plaintext resolves to nothing, not to the row it resembles.
	tok, err = repo.ResolveDashboardToken(ctx, "9rdt_test_deadbeefX")
	if err != nil || tok != nil {
		t.Errorf("wrong plaintext resolved: %+v err=%v", tok, err)
	}
	// The empty plaintext resolves to nothing rather than every row.
	tok, err = repo.ResolveDashboardToken(ctx, "")
	if err != nil || tok != nil {
		t.Errorf("empty plaintext resolved: %+v err=%v", tok, err)
	}

	// An invalid scope is refused at the write, not stored to fail later.
	if err := repo.CreateDashboardToken(ctx, "dtok_2", "bad", hash, keikey.LookupHash("other"), "root"); err == nil {
		t.Error("invalid scope accepted")
	}

	// Revocation stamps the row and resolution refuses it afterwards.
	if err := repo.RevokeDashboardToken(ctx, "dtok_1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	tok, err = repo.ResolveDashboardToken(ctx, plaintext)
	if err != nil || tok != nil {
		t.Errorf("revoked token resolved: %+v err=%v", tok, err)
	}
	// Revoking again is refused — the stamp is single-shot.
	if err := repo.RevokeDashboardToken(ctx, "dtok_1"); err == nil {
		t.Error("double revoke accepted")
	}

	// The list projection carries no hashes and shows the revocation.
	tokens, err := repo.ListDashboardTokens(ctx)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("list: %v (n=%d)", err, len(tokens))
	}
	if tokens[0].RevokedAt == "" {
		t.Error("list did not show the revocation")
	}
}
