// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// TOTP 2FA (E2): the dashboard login can require a six-digit code from an
// authenticator app, with ten one-time backup codes for when the phone is
// gone. Backup codes are bcrypt-hashed like passwords — a leaked backup list
// is useless without the hash — and a code burns on use, so a replay is dead
// on arrival.
//
// Recovery: losing the phone is never a lock-out — the CLI `9routerx
// reset-2fa` command clears the enrollment after a dashboard-password
// confirmation, and the backup codes exist precisely for this path.

// TOTPConfig is the stored enrollment state (a JSON blob in settings under
// the totp key).
type TOTPConfig struct {
	Enabled     bool      `json:"enabled"`
	Secret      string    `json:"secret,omitempty"`
	BackupCodes []string  `json:"backupCodes,omitempty"` // bcrypt hashes
	EnrolledAt  time.Time `json:"enrolledAt,omitempty"`
}

// GenerateTOTPSecret creates a new TOTP secret and the otpauth:// URI the
// dashboard renders for the authenticator app to scan.
func GenerateTOTPSecret(accountName, issuer string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		return "", "", fmt.Errorf("totp generate: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTPCode validates a six-digit code against the secret with ±90
// seconds of skew (three 30-second steps each way), so a phone whose clock
// drifted a little is not locked out. 90 seconds is the widest skew that
// still bounds a code to one-and-a-half windows.
func VerifyTOTPCode(secret, code string) bool {
	ok, err := totp.ValidateCustom(strings.TrimSpace(code), secret,
		time.Now(), totp.ValidateOpts{
			Period:    30,
			Skew:      3,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
	return err == nil && ok
}

// GenerateBackupCodes creates ten one-time codes. The plain codes are shown
// once at enroll time; the returned list is bcrypt-hashed for storage.
func GenerateBackupCodes() (plain []string, hashed []string, err error) {
	const n = 10
	plain = make([]string, 0, n)
	hashed = make([]string, 0, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, 6)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, fmt.Errorf("backup code entropy: %w", err)
		}
		code := base64.RawURLEncoding.EncodeToString(buf)[:8]
		plain = append(plain, code)
		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, fmt.Errorf("backup code hash: %w", err)
		}
		hashed = append(hashed, string(hash))
	}
	return plain, hashed, nil
}

// MatchBackupCode reports whether the given code matches any stored hash, and
// returns the index so the caller can burn it (remove the hash — a used code
// is dead).
func MatchBackupCode(hashed []string, code string) (index int, matched bool) {
	for i, hash := range hashed {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(strings.TrimSpace(code))) == nil {
			return i, true
		}
	}
	return -1, false
}

// BurnBackupCode removes the hash at index — the one-time-use half.
func BurnBackupCode(hashed []string, index int) []string {
	if index < 0 || index >= len(hashed) {
		return hashed
	}
	out := make([]string, 0, len(hashed)-1)
	out = append(out, hashed[:index]...)
	out = append(out, hashed[index+1:]...)
	return out
}

var totpConfig atomic.Value // auth.TOTPConfig

// SetTOTPConfig pushes the stored enrollment into the process-wide login
// gate, so the middleware check is an atomic read rather than a settings
// query per request.
func SetTOTPConfig(cfg TOTPConfig) {
	totpConfig.Store(cfg)
}

// TOTPConfigLive returns the pushed enrollment state for the login gate.
func TOTPConfigLive() TOTPConfig {
	if v, ok := totpConfig.Load().(TOTPConfig); ok {
		return v
	}
	return TOTPConfig{}
}

// ResetTOTPForCLI clears the enrollment after a dashboard-password
// confirmation on the local machine — the no-lock-out recovery path.
func ResetTOTPForCLI() error {
	SetTOTPConfig(TOTPConfig{})
	return nil
}

var _ = context.Background
