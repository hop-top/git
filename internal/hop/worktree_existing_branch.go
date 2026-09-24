package hop

import (
	"fmt"
	"strings"
)

// shortSHALen matches git's default abbreviation for display.
const shortSHALen = 7

// reconcileExistingBranch makes an existing refs/heads/<branch> honour an
// explicitly requested start-point before the worktree links it.
//
// Reports whether the local branch exists; when it does not, the caller must
// create it from startPoint rather than let `git worktree add <path> <branch>`
// guess a same-named remote branch.
//
// For an existing branch:
//   - at startPoint, or an ancestor of it: moved there with `git branch -f`,
//     so it fast-forwards and git applies its usual tracking rules
//     (branch.autoSetupMerge) exactly as for a freshly created branch. git
//     itself refuses when the branch is checked out in another worktree.
//   - ahead of or diverged from startPoint: error naming both commits; the
//     branch is left untouched so no local commit is ever discarded.
func (m *WorktreeManager) reconcileExistingBranch(dir, branch, startPoint string) (bool, error) {
	local, err := m.git.RevParse(dir, "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil || strings.TrimSpace(local) == "" {
		return false, nil
	}
	local = strings.TrimSpace(local)

	target, err := m.git.RevParse(dir, "--verify", "--quiet", startPoint+"^{commit}")
	if err != nil || strings.TrimSpace(target) == "" {
		return true, fmt.Errorf("start-point '%s' is not a valid commit", startPoint)
	}
	target = strings.TrimSpace(target)

	if local != target {
		base, err := m.git.MergeBase(dir, local, target)
		base = strings.TrimSpace(base)
		if err != nil || base != local {
			relation := "has diverged from"
			if err == nil && base == target {
				relation = "is ahead of"
			}
			return true, fmt.Errorf(
				"branch '%s' already exists at %s, which %s '%s' (%s)\n"+
					"hint: omit --from to use the existing branch as-is\n"+
					"hint: or reset it, discarding its local commits: git branch -f %s %s",
				branch, abbrev(local), relation, startPoint, abbrev(target),
				branch, startPoint)
		}
	}

	if _, err := m.git.RunInDir(dir, "git", "branch", "-f", branch, startPoint); err != nil {
		return true, fmt.Errorf("failed to move branch '%s' to '%s': %w", branch, startPoint, err)
	}
	return true, nil
}

func abbrev(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
