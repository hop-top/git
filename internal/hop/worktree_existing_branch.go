package hop

import (
	"fmt"
	"strings"

	"hop.top/git/internal/git"
)

// shortSHALen matches git's default abbreviation for display.
const shortSHALen = 7

// ExistingBranch is the verdict on how an explicitly requested start-point
// applies to refs/heads/<branch>. Local and Target are full commit IDs and
// are only set when Exists is true.
type ExistingBranch struct {
	Exists bool
	Local  string
	Target string
}

// FastForwards reports whether honouring the start-point moves the branch.
func (e ExistingBranch) FastForwards() bool {
	return e.Exists && e.Local != e.Target
}

// CheckExistingBranch decides, without writing anything, what honouring
// startPoint means for an existing refs/heads/<branch>:
//   - missing: Exists is false; the caller creates it from startPoint.
//   - at startPoint, or an ancestor of it: fine; FastForwards says whether
//     the branch has to move.
//   - ahead of or diverged from startPoint: an error naming both commits,
//     so no local commit is ever discarded.
func CheckExistingBranch(g git.GitInterface, dir, branch, startPoint string) (ExistingBranch, error) {
	local, err := g.RevParse(dir, "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil || strings.TrimSpace(local) == "" {
		return ExistingBranch{}, nil
	}
	e := ExistingBranch{Exists: true, Local: strings.TrimSpace(local)}

	target, err := g.RevParse(dir, "--verify", "--quiet", startPoint+"^{commit}")
	if err != nil || strings.TrimSpace(target) == "" {
		return e, fmt.Errorf("start-point '%s' is not a valid commit", startPoint)
	}
	e.Target = strings.TrimSpace(target)

	if e.Local == e.Target {
		return e, nil
	}
	base, err := g.MergeBase(dir, e.Local, e.Target)
	base = strings.TrimSpace(base)
	if err == nil && base == e.Local {
		return e, nil
	}
	relation := "has diverged from"
	if err == nil && base == e.Target {
		relation = "is ahead of"
	}
	return e, fmt.Errorf(
		"branch '%s' already exists at %s, which %s '%s' (%s)\n"+
			"hint: omit --from to use the existing branch as-is\n"+
			"hint: or reset it, discarding its local commits: git branch -f %s %s",
		branch, AbbrevSHA(e.Local), relation, startPoint, AbbrevSHA(e.Target),
		branch, startPoint)
}

// PreviewExistingBranch resolves startPoint and runs CheckExistingBranch
// exactly as CreateWorktree would with EnforceStartPoint set, but writes
// nothing. It also returns the resolved start-point, the name the real run
// reports.
func (m *WorktreeManager) PreviewExistingBranch(hopspace *Hopspace, hubPath, branch, startPoint, defaultBranch string) (ExistingBranch, string, error) {
	base := findBaseWorktree(hopspace, hubPath)
	resolved, _ := m.resolveStartPoint(base, startPoint, defaultBranch)
	e, err := CheckExistingBranch(m.git, base, branch, resolved)
	return e, resolved, err
}

// reconcileExistingBranch makes an existing refs/heads/<branch> honour an
// explicitly requested start-point before the worktree links it.
//
// Reports whether the local branch exists; when it does not, the caller must
// create it from startPoint rather than let `git worktree add <path> <branch>`
// guess a same-named remote branch.
//
// An existing branch that CheckExistingBranch accepts is moved there with
// `git branch -f`, so git applies its usual tracking rules
// (branch.autoSetupMerge) exactly as for a freshly created branch. git itself
// refuses when the branch is checked out in another worktree. A refused
// branch is left untouched.
func (m *WorktreeManager) reconcileExistingBranch(dir, branch, startPoint string) (bool, error) {
	e, err := CheckExistingBranch(m.git, dir, branch, startPoint)
	if err != nil || !e.Exists {
		return e.Exists, err
	}
	if _, err := m.git.RunInDir(dir, "git", "branch", "-f", branch, startPoint); err != nil {
		return true, fmt.Errorf("failed to move branch '%s' to '%s': %w", branch, startPoint, err)
	}
	return true, nil
}

// AbbrevSHA shortens a commit ID the way git displays it by default.
func AbbrevSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
