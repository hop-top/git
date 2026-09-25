package cmd

import (
	"fmt"
	"strings"

	"hop.top/git/internal/git"
)

// gitflowBranchSection is the git config section git-flow-next keeps a
// branch's own settings in, such as the base git flow start records:
// gitflow.branch.<branch>.*.
func gitflowBranchSection(branch string) string {
	return "gitflow.branch." + branch
}

// hasGitflowBranchConfig reports whether any gitflow.branch.<branch>.* key
// is set in the repository at dir. A key's subsection is everything
// between "gitflow." and its last dot, so the branch feat/x does not match
// the keys of feat/x.y.
func hasGitflowBranchConfig(g git.GitInterface, dir, branch string) bool {
	out, err := g.RunInDir(dir, "git", "config", "--name-only", "--get-regexp", `^gitflow\.branch\.`)
	if err != nil {
		return false
	}
	want := "branch." + branch
	for _, key := range strings.Split(out, "\n") {
		key = strings.TrimPrefix(strings.TrimSpace(key), "gitflow.")
		if i := strings.LastIndex(key, "."); i > 0 && key[:i] == want {
			return true
		}
	}
	return false
}

// rekeyGitflowBranchConfig moves gitflow.branch.<oldBranch>.* to
// gitflow.branch.<newBranch>.*, as `git branch -m` does for
// branch.<oldBranch>.*, so git flow finish still finds the renamed
// branch's recorded base. It reports whether there was anything to move.
// It refuses to merge into keys newBranch already has, leaving both.
func rekeyGitflowBranchConfig(g git.GitInterface, dir, oldBranch, newBranch string) (bool, error) {
	if !hasGitflowBranchConfig(g, dir, oldBranch) {
		return false, nil
	}
	if hasGitflowBranchConfig(g, dir, newBranch) {
		return false, fmt.Errorf("%s.* is already set; left %s.* as it is",
			gitflowBranchSection(newBranch), gitflowBranchSection(oldBranch))
	}
	if _, err := g.RunInDir(dir, "git", "config", "--rename-section",
		gitflowBranchSection(oldBranch), gitflowBranchSection(newBranch)); err != nil {
		return false, err
	}
	return true, nil
}
