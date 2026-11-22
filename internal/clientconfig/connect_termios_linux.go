//go:build linux

// Alyysha Router — source port and modifications by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package clientconfig

import "golang.org/x/sys/unix"

// Linux answers termios through the generic TCGETS/TCSETS pair. The BSDs use
// the older TIOC* names for the same operation; the constants cannot be shared
// because their values differ.
const (
	termiosGetReq = unix.TCGETS
	termiosSetReq = unix.TCSETS
)
