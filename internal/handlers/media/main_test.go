// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package media

import (
	"os"
	"testing"

	"alyysha/proxy/internal/testnet"
)

// ALLOW_PRIVATE_NETWORKING lets the handler tests reach their own httptest
// servers; testnet.Seal keeps those servers the only reachable destination.
func TestMain(m *testing.M) {
	os.Setenv("ALLOW_PRIVATE_NETWORKING", "true")
	testnet.Seal()
	os.Exit(m.Run())
}
