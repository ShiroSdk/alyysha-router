// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// TestTOTPEnrollVerify pins the happy path: enroll generates a secret and an
// otpauth URI, and a code generated from that secret validates.
func TestTOTPEnrollVerify(t *testing.T) {
	secret, uri, err := GenerateTOTPSecret("owner@alyysha", "Alyysha Router")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(uri, "otpauth://totp/Alyysha Router:") {
		t.Errorf("uri = %q, want an otpauth://totp URL", uri)
	}
	if !strings.Contains(uri, "secret="+secret) {
		t.Errorf("uri %q does not carry the secret", uri)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if !VerifyTOTPCode(secret, code) {
		t.Error("valid code rejected")
	}
	if VerifyTOTPCode(secret, "000000") {
		t.Error("wrong code accepted")
	}
}

// TestTOTPSkew pins (c): a code generated ±90 seconds away validates (the
// skew window), one ±2 minutes away does not.
func TestTOTPSkew(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("owner@alyysha", "Alyysha Router")
	if err != nil {
		t.Fatal(err)
	}
	nearPast := time.Now().Add(-90 * time.Second)
	codePast, err := totp.GenerateCode(secret, nearPast)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTPCode(secret, codePast) {
		t.Error("code from 90s ago rejected — skew window too tight")
	}
	farFuture := time.Now().Add(2 * time.Minute)
	codeFar, err := totp.GenerateCode(secret, farFuture)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyTOTPCode(secret, codeFar) {
		t.Error("code from 2 minutes ahead accepted — skew too wide")
	}
}

// TestBackupCodesBurn pins (e): codes hash on enroll, match on use, and burn
// exactly once — a reused code is dead.
func TestBackupCodesBurn(t *testing.T) {
	plain, hashed, err := GenerateBackupCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 10 || len(hashed) != 10 {
		t.Fatalf("codes = %d plain / %d hashed, want 10/10", len(plain), len(hashed))
	}
	// Every plain code hashes-match its list (sanity).
	for i, code := range plain {
		idx, matched := MatchBackupCode(hashed, code)
		if !matched || idx != i {
			t.Errorf("code %d: matched=%v idx=%d, want true/%d", i, matched, idx, i)
		}
	}
	// Burn one: it stops matching, others keep working.
	remaining := BurnBackupCode(hashed, 0)
	if _, matched := MatchBackupCode(remaining, plain[0]); matched {
		t.Error("burned code still matches")
	}
	if _, matched := MatchBackupCode(remaining, plain[1]); !matched {
		t.Error("non-burned code stopped matching")
	}
	// A wrong code matches nothing.
	if _, matched := MatchBackupCode(remaining, "wrongcod"); matched {
		t.Error("wrong code matched")
	}
}
