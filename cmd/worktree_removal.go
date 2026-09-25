package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// removeWorktreeFiles removes the worktree hop.json records at path, run
// from basePath, the way remove and merge remove one: through `git
// worktree remove`, and never by deleting the directory itself.
//
//   - a path git has registered goes through `git worktree remove
//     --force`; when git refuses (a locked worktree, a permission
//     error) the files stay and the error says so;
//   - a worktree another repository registered (its .git names that
//     repository) is removed through that repository the same way;
//   - a path no git has registered is removed only when it is gone
//     already or is an empty directory. Anything else in it is not a
//     worktree git-hop can vouch for (a worktree `git worktree move`
//     put inside it, say) and stays.
//
// A nil error means nothing is left at path.
func removeWorktreeFiles(fs afero.Fs, g git.GitInterface, basePath, path string) error {
	registered := isWorktreeRegistered(g, basePath, path)
	if !registered {
		if own, ok := worktreeRepository(g, path); ok {
			basePath, registered = own, true
		}
	}
	if registered {
		if err := g.WorktreeRemove(basePath, path, true); err != nil {
			return fmt.Errorf("git could not remove the worktree at '%s', so its files stay: %v", path, err)
		}
	} else {
		output.Debug("worktree %s not registered; skipping git worktree remove", path)
	}
	err := hop.NewCleanupManager(fs, g).RemoveEmptyDirectory(path)
	switch {
	case err == nil:
		return nil
	case registered:
		return fmt.Errorf("git removed the worktree at '%s', but something is left there, so it stays: %v", path, err)
	default:
		return fmt.Errorf("'%s' is not a worktree git has registered and is not empty, so it stays\n"+
			"hint: move out what you want to keep, delete it, then try again", path)
	}
}

// worktreeRepository returns the repository path, a worktree of which
// is rooted at path, when there is one: path is the top of a working
// tree, and that tree's repository is where `git worktree remove` for
// it runs. A path inside another tree, or in none, reports false.
func worktreeRepository(g git.GitInterface, path string) (string, bool) {
	out, err := g.RunInDir(path, "git", "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !state.SamePath(strings.TrimSpace(lines[0]), path) {
		return "", false
	}
	return strings.TrimSpace(lines[1]), true
}
