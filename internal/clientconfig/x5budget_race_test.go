//go:build race

// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package clientconfig

import "time"

// x5Budget under the race detector. See x5budget_norace_test.go.
const x5Budget = 30 * time.Second
