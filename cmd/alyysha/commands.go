// Alyysha Router — all source code port and modification by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"alyysha/proxy/internal/mitm"
)

func resolveDataDir() string {
	d := os.Getenv("DATA_DIR")
	if d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".alyysha"
	}
	return home + "/.alyysha"
}

func mitmEnable(_ *cli.Context) error {
	dataDir := resolveDataDir()
	mgr := mitm.NewManager(dataDir)
	if err := mgr.Enable(); err != nil {
		return fmt.Errorf("MITM enable failed: %w", err)
	}
	fmt.Println("MITM proxy enabled. Intercepted traffic on :443 → Alyysha Router proxy.")
	return nil
}

func mitmDisable(_ *cli.Context) error {
	dataDir := resolveDataDir()
	mgr := mitm.NewManager(dataDir)
	if err := mgr.Disable(); err != nil {
		return fmt.Errorf("MITM disable failed: %w", err)
	}
	fmt.Println("MITM proxy disabled.")
	return nil
}

func mitmStatus(_ *cli.Context) error {
	dataDir := resolveDataDir()
	mgr := mitm.NewManager(dataDir)
	status := mgr.Status()
	fmt.Printf("Running: %v\n", status["running"])
	fmt.Printf("CA installed: %v\n", status["ca_installed"])
	fmt.Printf("MITM dir: %v\n", status["mitm_dir"])
	return nil
}
