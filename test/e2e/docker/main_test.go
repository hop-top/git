//go:build dockere2e

package docker_test

import (
	"os"
	"testing"

	"hop.top/git/test/e2e"
)

// TestMain sweeps docker networks orphaned by a previous run before any test
// starts.
//
// These tests already tear their stacks down via t.Cleanup(CleanupContainers),
// which survives test failure and panic. What it cannot survive is the process
// being killed outright -- Ctrl-C, a CI timeout, SIGKILL -- and those orphaned
// networks accumulate until docker exhausts its predefined address pools.
// Sweeping before the first test keeps a run from inheriting that debris.
//
// This package builds its own test binary, so it needs its own TestMain; the
// one in package e2e does not run here.
func TestMain(m *testing.M) {
	e2e.SweepStaleNetworks()
	os.Exit(m.Run())
}
