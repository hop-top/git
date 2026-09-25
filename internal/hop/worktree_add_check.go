package hop

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"
)

// CheckAdd reports why CreateWorktreeTransactional would refuse to add
// branch at worktreePath, before anything is written. It only reads, so
// a preview can run it too and fail exactly as the real add would:
//   - branch is not a valid branch name;
//   - worktreePath is already a worktree: one the hopspace records, or
//     one git has registered, present or missing;
//   - branch is checked out in another worktree;
//   - something else occupies worktreePath: a file, or a directory that
//     is not empty. Like `git worktree add`, an empty directory is
//     reused; nothing at worktreePath is ever deleted.
func (m *WorktreeManager) CheckAdd(hopspace *Hopspace, hubPath, branch, worktreePath string) error {
	if err := checkBranchName(m, branch); err != nil {
		return err
	}

	validation, err := NewStateValidator(m.fs, m.git).ValidateWorktreeAdd(hopspace, hubPath, branch, worktreePath)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	if exists, _ := afero.Exists(m.fs, worktreePath); exists && validation.CanProceed {
		return fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	if err := m.checkRegistered(hopspace, hubPath, branch, worktreePath); err != nil {
		return err
	}
	if occupied(m.fs, worktreePath) {
		return fmt.Errorf("'%s' already exists and is not an empty directory\n"+
			"hint: move it away, or add another branch or set another hop.worktreeLocation", worktreePath)
	}
	return nil
}

// checkRegistered consults git's own registry: a worktree git-hop does
// not record, or whose directory is gone, still blocks `git worktree
// add`, as does branch checked out elsewhere. An unreadable list leaves
// the verdict to git.
func (m *WorktreeManager) checkRegistered(hopspace *Hopspace, hubPath, branch, worktreePath string) error {
	out, err := m.git.WorktreeListPorcelain(m.findBaseWorktree(hopspace, hubPath))
	if err != nil {
		return nil
	}
	for path, checkedOut := range porcelainBranches(out) {
		if samePath(path, worktreePath) {
			continue
		}
		if checkedOut == branch {
			return fmt.Errorf("'%s' is already checked out at '%s'", branch, path)
		}
	}
	for _, path := range parsePorcelainWorktrees(out) {
		if !samePath(path, worktreePath) {
			continue
		}
		if exists, _ := afero.Exists(m.fs, worktreePath); exists {
			return fmt.Errorf("worktree already exists at %s", worktreePath)
		}
		return fmt.Errorf("'%s' is a missing but already registered worktree\n"+
			"hint: clear it with 'git worktree prune', then retry", worktreePath)
	}
	return nil
}

// occupied reports whether something at path keeps `git worktree add`
// from creating a worktree there: anything but an empty directory. A
// directory that cannot be read counts as occupied.
func occupied(fs afero.Fs, path string) bool {
	info, err := fs.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	empty, err := afero.IsEmpty(fs, path)
	return err != nil || !empty
}

// CheckStartPoint reports why the add of branch would refuse its
// start-point: one named explicitly (not the default branch or
// 'initial') that does not resolve to a commit, for a branch the add
// creates from it. An existing local branch, or one that exists only on
// origin without EnforceStartPoint, is not created from startPoint (an
// existing branch with EnforceStartPoint is judged by
// CheckExistingBranch).
func (m *WorktreeManager) CheckStartPoint(hopspace *Hopspace, hubPath, branch, startPoint string) error {
	switch startPoint {
	case "", StartPointDefaultBranch, StartPointInitial:
		return nil
	}
	base := m.findBaseWorktree(hopspace, hubPath)
	if refResolves(m.git, base, "refs/heads/"+branch) {
		return nil
	}
	if !m.EnforceStartPoint && refResolves(m.git, base, "refs/remotes/origin/"+branch) {
		return nil
	}
	if _, err := m.git.RevParse(base, "--verify", "--quiet", startPoint+"^{commit}"); err != nil {
		return fmt.Errorf("start-point '%s' is not a valid commit", startPoint)
	}
	return nil
}

// checkBranchName applies git's rules for a new branch name
// (strbuf_check_branch_ref): no leading '-', not HEAD, and a valid
// refs/heads/<name>. check-ref-format needs no repository.
func checkBranchName(m *WorktreeManager, branch string) error {
	invalid := strings.HasPrefix(branch, "-") || branch == "HEAD"
	if !invalid {
		_, err := m.git.Run("git", "check-ref-format", "refs/heads/"+branch)
		invalid = err != nil
	}
	if invalid {
		return fmt.Errorf("'%s' is not a valid branch name", branch)
	}
	return nil
}
