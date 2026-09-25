package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
	basePath, registered := worktreeRemovalBase(g, basePath, path)
	if registered {
		if err := g.WorktreeRemove(basePath, path, true); err != nil {
			return gitRefusedRemoval(path, err)
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
		return unregisteredNotEmpty(path)
	}
}

// checkWorktreeRemoval is removeWorktreeFiles' preview: it changes
// nothing and returns the error removeWorktreeFiles would return for
// path, as far as that is known before git runs:
//
//   - a path no git has registered that holds anything is refused, as
//     the real run refuses it;
//   - a registered worktree git has locked is refused: the real run
//     passes --force once, and git removes a locked worktree only with
//     it twice;
//   - a registered worktree whose directory is there without a .git
//     file is refused: git's validation stops the removal.
//
// Uncommitted or untracked files are not among them: --force removes
// them, and the safety gate is what asks for them. What git or the
// filesystem would hit only while deleting the files (a permission or
// I/O error) cannot be known in advance and is not predicted.
func checkWorktreeRemoval(fs afero.Fs, g git.GitInterface, basePath, path string) error {
	basePath, registered := worktreeRemovalBase(g, basePath, path)
	if !registered {
		if emptyOrGone(fs, path) {
			return nil
		}
		return unregisteredNotEmpty(path)
	}
	if reason, locked := gitWorktreeLock(g, basePath, path); locked {
		cause := "it is locked"
		if reason != "" {
			cause = fmt.Sprintf("it is locked (%s)", reason)
		}
		return gitRefusedRemoval(path, fmt.Errorf("%s\nhint: run 'git worktree unlock %s' first", cause, path))
	}
	if exists, _ := afero.DirExists(fs, path); exists {
		dotGit := filepath.Join(path, ".git")
		info, err := fs.Stat(dotGit)
		switch {
		case os.IsNotExist(err):
			return gitRefusedRemoval(path, fmt.Errorf("'%s' does not exist", dotGit))
		case err == nil && !info.Mode().IsRegular():
			return gitRefusedRemoval(path, fmt.Errorf("'%s' is not a file", dotGit))
		}
	}
	return nil
}

// worktreeRemovalBase returns where `git worktree remove` for path runs,
// basePath or the repository another worktree's .git names, and whether
// any git has path registered at all.
func worktreeRemovalBase(g git.GitInterface, basePath, path string) (string, bool) {
	if isWorktreeRegistered(g, basePath, path) {
		return basePath, true
	}
	if own, ok := worktreeRepository(g, path); ok {
		return own, true
	}
	return basePath, false
}

// gitRefusedRemoval is the error for a worktree git will not remove.
func gitRefusedRemoval(path string, cause error) error {
	return fmt.Errorf("git could not remove the worktree at '%s', so its files stay: %v", path, cause)
}

// unregisteredNotEmpty is the error for a directory no git has
// registered that holds anything.
func unregisteredNotEmpty(path string) error {
	return fmt.Errorf("'%s' is not a worktree git has registered and is not empty, so it stays\n"+
		"hint: move out what you want to keep, delete it, then try again", path)
}

// emptyOrGone reports whether RemoveEmptyDirectory would remove path or
// find it gone already: nothing is there, or an empty directory is.
func emptyOrGone(fs afero.Fs, path string) bool {
	info, err := fs.Stat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || !info.IsDir() {
		return false
	}
	empty, err := afero.IsEmpty(fs, path)
	return err == nil && empty
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
