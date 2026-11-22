//go:build !race

// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package chat

// raceDetectorEnabled reports whether this test binary was built with -race.
// See usage_probe_race_test.go for why the latency scenarios care.
const raceDetectorEnabled = false
