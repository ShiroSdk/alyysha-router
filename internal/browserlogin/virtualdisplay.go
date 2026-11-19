// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package browserlogin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Virtual display (D): a Linux host with no display — a Docker image, a VPS
// over SSH — cannot open the headed browser a web-login needs. Xvfb gives
// that host a display to launch against: useful exactly when the provider's
// profile is persistent, where a warm session lets the flow capture without
// human interaction. Providers that need the operator to type credentials
// (gemini-web's Google sign-in) stay refused on a displayless host: an
// invisible browser cannot be signed in to.

// MISSING_DISPLAY_MESSAGE is the refusal text for a provider that needs the
// operator's eyes.
var MISSING_DISPLAY_MESSAGE = "Missing X server or $DISPLAY: a headed browser needs a display. Install Xvfb (apt-get install xvfb) or run with xvfb-run, or paste the credential manually."

// xvfbStartTimeout bounds the wait for Xvfb to report its display number.
const xvfbStartTimeout = 5 * time.Second

// xvfbArgs is the argument vector the virtual display runs with: -displayfd
// makes Xvfb pick a free display and print the number to the file
// descriptor the parent supplies.
func xvfbArgs() []string {
	return []string{"-displayfd", "3", "-screen", "0", "1280x1024x24", "-nolisten", "tcp"}
}

// parseDisplayFDOutput reads the display number Xvfb prints to the displayfd
// pipe: a line of digits, possibly with trailing whitespace. Empty when the
// output carries no number yet.
func parseDisplayFDOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if n := 0; len(line) > 0 && isDigits(line) {
			_, _ = fmt.Sscanf(line, "%d", &n)
			if n >= 0 {
				return fmt.Sprintf(":%d", n)
			}
		}
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type virtualDisplay struct {
	cmd     *exec.Cmd
	display string
}

var (
	virtualDisplayMu     sync.Mutex
	virtualDisplaySingle sync.Once
	virtualDisplayErr    error
	virtualDisplayCur    *virtualDisplay
	// virtualDisplayStarter is the seam tests stand in for; production runs
	// startXvfbProcess.
	virtualDisplayStarter = startXvfbProcess
)

// startXvfbProcess spawns Xvfb and reads the display number it prints.
func startXvfbProcess() (*virtualDisplay, error) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		return nil, errors.New("Xvfb not found — install it (apt-get install xvfb) to run a web-login on a displayless host")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create the displayfd pipe: %w", err)
	}
	defer func() { _ = reader.Close() }()

	cmd := exec.Command("Xvfb", xvfbArgs()...)
	cmd.ExtraFiles = []*os.File{writer} // the child's fd 3
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("Xvfb exited — %w", err)
	}
	_ = writer.Close() // the parent only reads; EOF means Xvfb died

	displayCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		var acc strings.Builder
		for {
			n, rerr := reader.Read(buf)
			if n > 0 {
				acc.Write(buf[:n])
				if display := parseDisplayFDOutput(acc.String()); display != "" {
					displayCh <- display
					return
				}
			}
			if rerr != nil {
				errCh <- errors.New("Xvfb exited before reporting a display")
				return
			}
		}
	}()

	select {
	case display := <-displayCh:
		return &virtualDisplay{cmd: cmd, display: display}, nil
	case err := <-errCh:
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("Xvfb exited — %w", err)
	case <-time.After(xvfbStartTimeout):
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, errors.New("Xvfb start timeout")
	}
}

// ensureVirtualDisplay returns a running virtual display on a displayless
// Linux host, reusing the one it already started. Any other platform — and
// any host with a real display — returns nil, meaning "no virtual display
// needed".
func ensureVirtualDisplay(ctx context.Context) (display string, stop func(), err error) {
	if runtime.GOOS != "linux" {
		return "", nil, nil
	}
	if hasDisplayEnv() {
		return "", nil, nil
	}
	virtualDisplayMu.Lock()
	defer virtualDisplayMu.Unlock()
	if virtualDisplayCur != nil {
		return virtualDisplayCur.display, stopVirtualDisplay, nil
	}
	started := false
	virtualDisplaySingle.Do(func() {
		cur, err2 := virtualDisplayStarter()
		if err2 != nil {
			virtualDisplayErr = err2
			return
		}
		virtualDisplayCur = cur
		started = true
	})
	if virtualDisplayCur == nil {
		return "", nil, virtualDisplayErr
	}
	_ = started
	return virtualDisplayCur.display, stopVirtualDisplay, nil
}

func stopVirtualDisplay() {
	virtualDisplayMu.Lock()
	defer virtualDisplayMu.Unlock()
	if virtualDisplayCur != nil && virtualDisplayCur.cmd.Process != nil {
		_ = virtualDisplayCur.cmd.Process.Kill()
	}
	virtualDisplayCur = nil
}

// hasDisplayEnv reports whether the process environment names a usable
// display.
func hasDisplayEnv() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// displayEnvWithVirtual returns the launch environment for a displayless
// Linux host: the resolved environment plus the virtual display's DISPLAY.
func displayEnvWithVirtual(base []string, display string) []string {
	for _, entry := range base {
		if strings.HasPrefix(entry, "DISPLAY=") {
			return base
		}
	}
	return append(append([]string{}, base...), "DISPLAY="+display)
}

var _ = context.Background
