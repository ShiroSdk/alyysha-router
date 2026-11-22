// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package browserlogin

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// The virtual-display unit checks (D): the argument vector, the displayfd
// number parse, and the environment patching — everything that does not
// need an Xvfb binary (CI carries none; the integration lives on the host).

func TestXvfbArgs(t *testing.T) {
	args := xvfbArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-displayfd 3") {
		t.Errorf("displayfd missing: %v", args)
	}
	if !strings.Contains(joined, "-nolisten tcp") {
		t.Errorf("nolisten tcp missing: %v", args)
	}
	if !strings.Contains(joined, "-screen 0 1280x1024x24") {
		t.Errorf("screen missing: %v", args)
	}
}

func TestParseDisplayFDOutput(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"99\n", ":99"},
		{"  7  \n", ":7"},
		{"12", ":12"},
		{"", ""},
		{"xvfb starting\n", ""},
	}
	for _, tc := range cases {
		if got := parseDisplayFDOutput(tc.in); got != tc.want {
			t.Errorf("parseDisplayFDOutput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDisplayEnvWithVirtual(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root"}
	got := displayEnvWithVirtual(base, ":7")
	if len(got) != 3 || got[2] != "DISPLAY=:7" {
		t.Errorf("env = %v", got)
	}
	// An environment that already names a display keeps it.
	withDisplay := []string{"PATH=/usr/bin", "DISPLAY=:0"}
	got = displayEnvWithVirtual(withDisplay, ":7")
	if len(got) != 2 || !strings.Contains(strings.Join(got, " "), "DISPLAY=:0") {
		t.Errorf("existing DISPLAY overridden: %v", got)
	}
}

// A real display short-circuits the whole mechanism on every platform: the
// environment names a display, so nothing is started.
func TestEnsureVirtualDisplaySkipsRealDisplays(t *testing.T) {
	t.Setenv("DISPLAY", ":5")
	display, stop, err := ensureVirtualDisplay(context.Background())
	if err != nil {
		t.Fatalf("ensure errored: %v", err)
	}
	if display != "" || stop != nil {
		t.Errorf("a real display started a virtual one: %q", display)
	}
}

// On Windows the desktop always has a display: the mechanism never runs.
func TestEnsureVirtualDisplayNeverRunsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only assertion")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	display, stop, err := ensureVirtualDisplay(context.Background())
	if err != nil {
		t.Fatalf("ensure errored: %v", err)
	}
	if display != "" || stop != nil {
		t.Errorf("windows started a virtual display: %q", display)
	}
}
