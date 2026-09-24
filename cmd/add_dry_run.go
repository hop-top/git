package cmd

import (
	"path/filepath"
	"strings"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// addPlan is everything `git hop add` has decided before its first write.
type addPlan struct {
	cwd           string
	hubPath       string
	hopspace      *hop.Hopspace
	repoID        string
	branch        string
	worktreePath  string
	startPoint    string
	defaultBranch string
	fetch         bool
	task          string
}

// addHooks are the lifecycle hooks add dispatches, in order.
var addHooks = []string{"pre-worktree-add", "post-worktree-add"}

// previewAdd reports what `git hop add` would do for p without doing any
// of it. Reads only: no worktree, branch, hop.json, state, symlink or
// dependency writes, and no hook or branch-type detector runs, since
// either may mutate the repo.
func previewAdd(g git.GitInterface, wm *hop.WorktreeManager, hookRunner *hooks.Runner, p addPlan) {
	if p.fetch {
		output.Info("[dry-run] Would fetch origin")
	}
	if wm.EnforceStartPoint {
		previewEnforcedBranch(wm, p)
	} else {
		previewBranch(g, p)
	}

	output.Info("[dry-run] Would create worktree at %s", displayPath(p.cwd, p.worktreePath))
	if p.task != "" {
		output.Info("[dry-run] Would record task '%s' for '%s'", p.task, p.branch)
	}

	for _, name := range addHooks {
		if f := hookRunner.FindHookFile(name, p.worktreePath, p.repoID); f != "" {
			output.Info("[dry-run] Would run hook %s (%s)", name, f)
		}
	}

	output.Info("[dry-run] Would register '%s' in hop.json and point 'current' at it", p.branch)
}

// previewBranch reports how add links or creates the branch when the
// start-point only seeds new branches: an existing local or remote branch
// is checked out as-is.
func previewBranch(g git.GitInterface, p addPlan) {
	switch {
	case refExists(g, p.hubPath, "refs/heads/"+p.branch):
		output.Info("[dry-run] Would check out existing branch '%s'", p.branch)
	case refExists(g, p.hubPath, "refs/remotes/origin/"+p.branch):
		output.Info("[dry-run] Would check out existing branch '%s' (tracking 'origin/%s')", p.branch, p.branch)
	default:
		output.Info("[dry-run] Would create branch '%s' from %s", p.branch, describeStartPoint(p.startPoint, p.defaultBranch))
	}
}

// previewEnforcedBranch reports how add honours an explicit --from: an
// existing local branch is fast-forwarded or refused (see
// hop.CheckExistingBranch), and a missing one is created from the
// start-point, never from a same-named remote branch. A refusal fails the
// preview with the real run's message and exit status.
func previewEnforcedBranch(wm *hop.WorktreeManager, p addPlan) {
	e, target, err := wm.PreviewExistingBranch(p.hopspace, p.hubPath, p.branch, p.startPoint, p.defaultBranch)
	if err != nil {
		output.Fatal("Failed to create worktree: %v", err)
	}
	switch {
	case !e.Exists:
		output.Info("[dry-run] Would create branch '%s' from %s", p.branch, describeStartPoint(p.startPoint, p.defaultBranch))
	case e.FastForwards():
		output.Info("[dry-run] Would fast-forward existing branch '%s' from %s to '%s' (%s)",
			p.branch, hop.AbbrevSHA(e.Local), target, hop.AbbrevSHA(e.Target))
	default:
		output.Info("[dry-run] Would check out existing branch '%s' (already at '%s', %s)",
			p.branch, target, hop.AbbrevSHA(e.Target))
	}
}

// describeStartPoint renders the start-point the way WorktreeManager
// will interpret it (see hop.WorktreeManager.CreateWorktree).
func describeStartPoint(startPoint, defaultBranch string) string {
	switch startPoint {
	case "", hop.StartPointDefaultBranch:
		if defaultBranch == "" {
			return "HEAD"
		}
		return "default branch '" + defaultBranch + "'"
	case hop.StartPointInitial:
		return "the root commit ('initial')"
	default:
		return "'" + startPoint + "'"
	}
}

func refExists(g git.GitInterface, dir, ref string) bool {
	_, err := g.RevParse(dir, "--verify", "--quiet", ref)
	return err == nil
}

// displayPath renders path relative to cwd, the form add prints for the
// new worktree.
func displayPath(cwd, path string) string {
	rel, _ := filepath.Rel(cwd, path)
	if !strings.HasPrefix(rel, ".") && !filepath.IsAbs(rel) {
		rel = "./" + rel
	}
	return rel
}
