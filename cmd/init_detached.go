package cmd

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// refuseDetachedHead exits, as git does, when no branch is checked out:
// every init mode names something after the current branch. It runs
// before any backup and before the dry-run plan, for every layout, and
// --force does not change it. When a paused operation (a rebase, most
// often) is what detached HEAD, the advice is to conclude it instead.
func refuseDetachedHead(fs afero.Fs, g git.GitInterface, repoPath string) {
	var dhe *hop.DetachedHeadError
	if _, err := hop.CurrentBranchForConversion(g, repoPath); !errors.As(err, &dhe) {
		return
	}
	hints := dhe.Hints()
	if ops := hop.InProgressOperations(fs, filepath.Join(repoPath, ".git")); len(ops) > 0 {
		hints = (&hop.InProgressError{Ops: ops}).Hints()
	}
	output.Hint("%s", strings.Join(hints, "\n"))
	output.Fatal("%s", dhe.Error())
}
