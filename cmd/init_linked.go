package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// isHubStructure reports whether s is a git-hop hub's root: a bare
// repository, or a regular one holding hop.json.
func isHubStructure(s config.StructureType) bool {
	return s == config.BareWorktreeRoot || s == config.WorktreeRoot
}

// repoOfLinkedWorktree returns the root of the repository the linked
// worktree at dir belongs to, from git's common dir, and that root's
// structure. ok is false when git does not report dir as a linked
// worktree.
func repoOfLinkedWorktree(fs afero.Fs, g git.GitInterface, dir string) (string, config.StructureType, bool) {
	root, ok := hop.RepoRootOfWorktree(g, dir)
	if !ok {
		return "", config.UnknownStructure, false
	}
	return root, hop.DetectRepoStructure(fs, g, root), true
}

// resolveInitTarget decides which repository init acts on. Run from a
// linked worktree of a repository that is not a hub yet, init acts on
// that repository, as git commands run from a linked worktree do: it is
// offered the conversion, which carries its linked worktrees, instead of
// being reported as already initialized. A linked
// worktree of a hub, and anything else, is left as detected.
func resolveInitTarget(fs afero.Fs, g git.GitInterface, cwd string, s config.StructureType) (string, config.StructureType) {
	if s != config.WorktreeChild {
		return cwd, s
	}
	root, rs, ok := repoOfLinkedWorktree(fs, g, cwd)
	if !ok || isHubStructure(rs) {
		return cwd, s
	}
	output.Note("%s is a linked worktree of %s; init acts on that repository", cwd, root)
	return root, rs
}

// planInitLinkedCarry decides what a bare conversion of repoPath does
// with its linked worktrees, and exits, as git does, when one cannot be
// carried: every reason as an error, then the advice. It runs before
// any backup and before the dry-run plan; --force does not change it.
func planInitLinkedCarry(fs afero.Fs, g git.GitInterface, repoPath string) *hop.LinkedCarryPlan {
	plan, err := hop.PlanLinkedCarry(fs, g, repoPath)
	var le *hop.LinkedWorktreesError
	if errors.As(err, &le) {
		for _, p := range le.Problems {
			output.Error("%s", p)
		}
		output.Hint("Resolve the above, then run %s again.\n"+
			"To convert to the regular layout instead, which keeps .git in place:\n  %s",
			initRetryCommand(initRunFlags), initRegularCommand(initRunFlags))
		output.Fatal("%s", le.Error())
	}
	if err != nil {
		output.Fatal("%v", err)
	}
	return plan
}

// printCarriedWorktrees lists the linked worktrees a bare conversion
// carried, each where it is now.
func printCarriedWorktrees(carried []config.CarriedWorktree) {
	if len(carried) == 0 {
		return
	}
	fmt.Println("Linked worktrees carried into the hub:")
	for _, w := range carried {
		what := "detached HEAD"
		if w.Branch != "" {
			what = w.Branch
		}
		if w.MovedFrom != "" {
			fmt.Printf("  %s  (%s, moved from %s)\n", w.Path, what, w.MovedFrom)
			continue
		}
		fmt.Printf("  %s  (%s)\n", w.Path, what)
	}
}
