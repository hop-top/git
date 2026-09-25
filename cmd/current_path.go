package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/shell"
)

// currentPathCmd prints the absolute path of the hub's current worktree, or
// nothing at all when the caller is not inside a hub.
//
// Hidden, following __notify-chdir's precedent, because it is plumbing: the
// installed shell integration calls it to find where to cd after a switch,
// and a person typing it by hand is better served by `git hop status`.
//
// It exists because the wrapper cannot answer this question in shell. The
// wrapper used to try, resolving the hub relative to `git rev-parse
// --show-toplevel`, and that is wrong in both directions. Inside a worktree
// the toplevel IS the worktree, and the hub sits a number of levels above it
// that depends on how deep the branch name nests -- "main" is one level
// under hops/, "a/b/c" is three, and no fixed count of `..` is right for
// both. From the bare hub itself there is no toplevel at all: rev-parse
// exits 128 and prints nothing, so a resolution keyed on it has nothing to
// key on and the cd was skipped silently.
//
// Both cases have one correct answer -- walk up for hop.json -- and that
// walk already exists here as hop.FindHub, tested, and used by every other
// command that needs a hub. Reimplementing it in three shells would mean
// three chances to get the walk wrong against one authority that is already
// right. The fork this costs is affordable where the equivalent fork in the
// chdir handler would not be: this runs once per `git hop` the user types,
// not once per prompt they draw.
//
// Silence plus exit 0 is the "no hub here" answer, deliberately not an
// error. The wrapper runs this after every eligible invocation, including
// ones made from an ordinary repository that has nothing to do with git-hop,
// and an empty result there is a correct and unremarkable outcome rather
// than a failure worth a message on the user's terminal.
var currentPathCmd = &cobra.Command{
	Use:    shell.CurrentPathCommand,
	Short:  "Print the absolute path of the hub's current worktree",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		path, ok := resolveCurrentWorktree(afero.NewOsFs(), cwd)
		if !ok {
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return nil
	},
}

// resolveCurrentWorktree reports the absolute path the hub above dir points
// its `current` symlink at, and whether that path is a directory that
// exists.
//
// The existence check is the whole reason this returns a bool rather than a
// string the caller tests: `current` is a symlink git-hop maintains, and a
// worktree removed outside git-hop leaves it dangling. Printing a dangling
// target would hand the wrapper a path to cd into that is not there, which
// is a worse failure than printing nothing -- the shell would report an
// error the user did not cause and cannot act on.
func resolveCurrentWorktree(fs afero.Fs, dir string) (string, bool) {
	hubPath, err := hop.FindHub(fs, dir)
	if err != nil {
		return "", false
	}

	target, err := hop.GetCurrentSymlink(fs, hubPath)
	if err != nil {
		return "", false
	}

	// The symlink is written relative to the hub for portability, so it is
	// resolved against the hub rather than against the caller's cwd.
	if !filepath.IsAbs(target) {
		target = filepath.Join(hubPath, target)
	}

	info, err := fs.Stat(target)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return target, true
}

func init() {
	cli.RootCmd.AddCommand(currentPathCmd)
}
