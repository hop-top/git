package cmd

import (
	"testing"

	"hop.top/git/internal/cli"
)

// Diagnosing and repairing hubs and state is kit's MANAGEMENT group; every
// other listed command is in kit's default group.
func TestCommandGroups(t *testing.T) {
	management := map[string]bool{"doctor": true, "repair": true, "prune": true}
	for _, c := range cli.RootCmd.Commands() {
		if c.Hidden {
			continue
		}
		want := ""
		if management[c.Name()] {
			want = cli.GroupManagement
		}
		if c.GroupID != want {
			t.Errorf("%s: group %q, want %q", c.Name(), c.GroupID, want)
		}
		delete(management, c.Name())
	}
	for name := range management {
		t.Errorf("%s: not a listed command", name)
	}
}
