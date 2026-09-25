package cmd

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// checkRepoIDCollisions reports the repositories state still records
// under a key their origin does not give (state.RepoIDCollisions): loading
// state moves each repository to the key "<host>/<org>/<repo>" of its
// origin, but leaves one where it is when that key is already taken
// rather than merge two entries on its own. Each is an issue: commands
// run in the hub read and write the entry under the new key, so the old
// one goes stale. --fix leaves it too; the hint says how to merge by
// hand.
func checkRepoIDCollisions(fs afero.Fs, st *state.State, r *doctorReport) {
	if st == nil {
		return
	}
	stateFile := filepath.Join(state.GetStateHome(), "state.json")
	for _, c := range state.RepoIDCollisions(fs, st) {
		hubs := strings.Join(c.Hubs, ", ")
		output.Error("state records %s under %s, but its origin gives %s, which state already holds; both entries kept", hubs, c.From, c.To)
		output.Hint("merge by hand: copy %s, then in it move those hubs and their worktrees from %q into %q and delete %q",
			stateFile, c.From, c.To, c.From)
		r.issue(doctorCheckState, c.From,
			"%s recorded under %s, but its origin gives %s, which state already holds; both entries kept; merge by hand: move those hubs and their worktrees into %s in %s and delete %s",
			hubs, c.From, c.To, c.To, stateFile, c.From)
	}
}
