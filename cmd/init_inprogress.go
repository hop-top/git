package cmd

import (
	"errors"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// refuseOperationInProgress exits, as git does, when a bare conversion
// would abandon a merge, rebase, am, cherry-pick, revert or bisect in
// progress: the advice first, then the fatal, and status 1 like the
// dirty-tree refusal. --force and --dry-run do not change it.
func refuseOperationInProgress(fs afero.Fs, repoPath string) {
	var ipe *hop.InProgressError
	if !errors.As(hop.CheckNoOperationInProgress(fs, repoPath), &ipe) {
		return
	}
	output.Hint("%s", strings.Join(ipe.Hints(), "\n"))
	output.Fatal("%s", ipe.Error())
}
