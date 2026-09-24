package config_test

import (
	"os"
	"testing"

	"hop.top/git/internal/testenv"
)

// TestMain keeps this package's tests off the developer's real config,
// data, state and cache homes and global git config; see testenv.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}
