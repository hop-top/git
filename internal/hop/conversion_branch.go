package hop

import (
	"fmt"

	"hop.top/git/internal/git"
)

// DetachedHeadError is the refusal of a conversion while no branch is
// checked out. Both layouts name what they create after the current
// branch: the bare layout's hops/<branch> worktree, the regular layout's
// hop.json entry for the repository root.
type DetachedHeadError struct{}

func (e *DetachedHeadError) Error() string {
	return "HEAD is detached; git hop init names the hub's worktree after the current branch"
}

// Hints tells the user how to get onto a branch, then how to retry.
func (e *DetachedHeadError) Hints() []string {
	return []string{
		"check out a branch first, or create one at this commit:",
		"  git switch <branch>",
		"  git switch -c <new-branch>",
		"then run git hop init again",
	}
}

// CurrentBranchForConversion returns the branch checked out at repoPath,
// or a *DetachedHeadError when HEAD is detached. A failure to ask git is
// returned wrapped.
func CurrentBranchForConversion(g git.GitInterface, repoPath string) (string, error) {
	branch, err := g.GetCurrentBranch(repoPath)
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	if branch == "" {
		return "", &DetachedHeadError{}
	}
	return branch, nil
}
