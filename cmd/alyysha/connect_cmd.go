// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package main

import (
	"fmt"
	"strings"

	"alyysha/proxy/internal/clientconfig"

	"github.com/urfave/cli/v2"
)

// connectOptionsFromFlags assembles the adapter options from the CLI flags.
// The adapter decides the file shapes; the flags carry the three things
// connect is allowed to write (endpoint, credential, model) plus the home
// override for tests and portable setups.
func connectOptionsFromFlags(cCtx *cli.Context) clientconfig.Options {
	return clientconfig.Options{
		Home:    cCtx.String("home"),
		BaseURL: cCtx.String("base-url"),
		APIKey:  cCtx.String("api-key"),
		Model:   cCtx.String("model"),
	}
}

// renderPlan prints the resolved edits the way a --dry-run shows them: file,
// key path, and the value for non-credential edits. The credential never
// prints — it is the one secret connect carries.
func renderPlan(plans []clientconfig.FilePlan, opts clientconfig.Options) string {
	var b strings.Builder
	for _, plan := range plans {
		fmt.Fprintf(&b, "file: %s\n", plan.File.Path)
		if plan.RemoveFile {
			fmt.Fprintf(&b, "  (removed)\n")
			continue
		}
		for _, edit := range plan.Edits {
			keyPath := strings.Join(edit.KeyPath, ".")
			value := edit.Value
			if strings.Contains(strings.ToLower(keyPath), "token") ||
				strings.Contains(strings.ToLower(keyPath), "api") ||
				strings.Contains(strings.ToLower(keyPath), "auth") {
				value = maskConnectValue(value)
			}
			action := "set"
			if edit.Delete {
				action = "delete"
			}
			fmt.Fprintf(&b, "  %s %s = %s\n", action, keyPath, value)
		}
	}
	_ = opts
	return b.String()
}

// maskConnectValue keeps the first four and the last four characters, so the
// operator can tell which credential landed without printing it.
func maskConnectValue(v string) string {
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + strings.Repeat("*", len(v)-8) + v[len(v)-4:]
}

// newConnectCommand builds the `alyysha connect <client>` command: it patches
// the named AI client's config to point at this gateway — endpoint, credential
// and default model only, never anything else (the hooks guard refuses a
// payload that carries more).
func newConnectCommand() *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "api-key", Usage: "The gateway API key to write into the client config"},
		&cli.StringFlag{Name: "base-url", Value: "http://127.0.0.1:20128", Usage: "The gateway base URL the client should call"},
		&cli.StringFlag{Name: "model", Usage: "Default model the client should request"},
		&cli.StringFlag{Name: "home", Usage: "Override the home directory the config is resolved under"},
		&cli.BoolFlag{Name: "dry-run", Usage: "Show the resolved edits without writing anything"},
		&cli.BoolFlag{Name: "create", Usage: "Create the config file when it is missing (connect never auto-creates without it)"},
	}
	return &cli.Command{
		Name:      "connect",
		Usage:     "Point an AI client's config at this gateway",
		ArgsUsage: "<client>  (claude-code, codex, opencode, droid, crush, kilo, cline, pi, omp)",
		Flags:     flags,
		Action:    runConnect,
	}
}

func runConnect(cCtx *cli.Context) error {
	clientName := cCtx.Args().First()
	if clientName == "" {
		return fmt.Errorf("connect: name the client to configure (try `alyysha connect --help`)")
	}
	adapter, err := clientconfig.Resolve(clientName)
	if err != nil {
		// The registry knows the nine names; a typo lands here.
		return fmt.Errorf("connect: unknown client %q — known clients are listed in `alyysha connect --help`", clientName)
	}

	opts := connectOptionsFromFlags(cCtx)
	if opts.APIKey == "" {
		return fmt.Errorf("connect: --api-key is required (the credential connect writes)")
	}

	paths, err := adapter.Paths(opts)
	if err != nil {
		return err
	}
	for _, file := range paths.Files {
		fmt.Printf("target: %s\n", file.Path)
	}

	plans, err := adapter.Plan(opts)
	if err != nil {
		// A config the adapter cannot parse (corrupt JSONC and friends) ends
		// here with the parse error — clear and non-zero, never a panic.
		return err
	}
	if err := clientconfig.AssertNoForbiddenEdits(plans); err != nil {
		return err
	}

	if cCtx.Bool("dry-run") {
		fmt.Println("dry-run — no file was written:")
		fmt.Print(renderPlan(plans, opts))
		return nil
	}

	result, err := adapter.Apply(opts)
	if err != nil {
		return err
	}
	if !result.Changed {
		fmt.Println("already connected — nothing to write.")
		return nil
	}
	for _, backup := range result.Backups {
		fmt.Printf("backup: %s\n", backup)
	}
	fmt.Println("[OK] client config updated — the gateway endpoint, credential and model are set.")
	return nil
}
