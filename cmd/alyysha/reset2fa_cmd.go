// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package main

import (
	"fmt"
	"os"

	"alyysha/proxy/internal/auth"
	"alyysha/proxy/internal/config"
	"alyysha/proxy/internal/db"

	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/bcrypt"
)

// newReset2FACommand builds the `alyysha reset-2fa` recovery command: it
// clears the TOTP enrollment after a dashboard-password confirmation, so a
// lost phone is never a permanent lock-out. The command reads the same
// database the gateway runs, and the confirmation is the dashboard password
// itself — an attacker with shell access to the DATA_DIR has the database
// either way.
func newReset2FACommand() *cli.Command {
	return &cli.Command{
		Name:  "reset-2fa",
		Usage: "Clear the dashboard TOTP enrollment (recovery for a lost authenticator)",
		Action: func(cCtx *cli.Context) error {
			cfg := config.LoadConfig()
			database, err := db.OpenDatabase(cfg.DatabasePath)
			if err != nil {
				return fmt.Errorf("open database at %s: %w", cfg.DatabasePath, err)
			}
			defer database.Close()
			repo := db.NewRepo(database)

			raw, err := repo.GetSettingsRaw()
			if err != nil {
				return fmt.Errorf("read settings: %w", err)
			}
			hash := ""
			if blob, ok := raw["dashboardPasswordHash"].(string); ok {
				hash = blob
			}
			fmt.Print("Dashboard password: ")
			var password string
			if _, err := fmt.Scanln(&password); err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			if hash == "" {
				// No stored hash: the dashboard is running with the default
				// password, which reset cannot confirm — refuse rather than
				// clear the enrollment for free.
				return fmt.Errorf("no dashboard password hash stored; set INITIAL_PASSWORD or the dashboard password first")
			}
			if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
				return fmt.Errorf("wrong dashboard password — 2FA enrollment left untouched")
			}

			if blob, ok := raw["totp"].(map[string]any); ok && len(blob) > 0 {
				if err := repo.UpdateSettingsRaw(map[string]any{"totp": auth.TOTPConfig{}}); err != nil {
					return fmt.Errorf("clear enrollment: %w", err)
				}
				auth.SetTOTPConfig(auth.TOTPConfig{})
				fmt.Println("[OK] TOTP enrollment cleared — log in with the dashboard password and re-enroll.")
				return nil
			}
			fmt.Println("TOTP is not enrolled — nothing to reset.")
			return nil
		},
	}
}

var _ = os.Getenv
