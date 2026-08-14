package e2e

import (
	"os"
	"testing"
)

// TestMain sweeps docker networks orphaned by a previous run before any test
// starts, then runs the suite.
//
// The sweep belongs here and nowhere else. Per-test t.Cleanup handles the
// normal path, including failures and panics, but cannot run at all when the
// process is killed outright -- Ctrl-C, a CI timeout, SIGKILL. Those runs
// leave networks behind, and they accumulate silently until docker exhausts
// its predefined address pools, at which point an unrelated test fails with a
// docker-internal message that looks nothing like the real cause.
//
// TestMain also runs strictly before the first test, which is what makes a
// global sweep safe: the suite runs tests in parallel, so sweeping at any
// later point could delete a network belonging to a test still using it.
func TestMain(m *testing.M) {
	SweepStaleNetworks()
	os.Exit(m.Run())
}
