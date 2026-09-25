package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// addPlan is everything `git hop add` has decided before its first write.
type addPlan struct {
	cwd           string
	fs            afero.Fs
	hubPath       string
	hopspace      *hop.Hopspace
	hubConfig     *config.HubConfig
	repoID        string
	branch        string
	worktreePath  string
	startPoint    string
	defaultBranch string
	fetch         bool
	task          string
	envStart      bool
}

// addHooks are the lifecycle hooks add dispatches, in order.
var addHooks = []string{"pre-worktree-add", "post-worktree-add"}

// previewAdd reports what `git hop add` would do for p without doing any
// of it, and returns the result the add would produce. Reads only: no
// worktree, branch, hop.json, state, symlink or dependency writes, and no
// hook or branch-type detector action runs, since either may mutate the
// repo.
func previewAdd(g git.GitInterface, wm *hop.WorktreeManager, hookRunner *hooks.Runner, p addPlan) addResult {
	// Probed first: the result reports the branch as it is before the add.
	res := previewAddResult(g, p)
	previewRefusals(wm, p)

	if p.fetch {
		output.Info("[dry-run] Would fetch origin")
	}
	started, err := previewGitflowStart(g, wm, p)
	if err != nil {
		refusePreview(p, fmt.Errorf("branch type detector failed: %v", err))
	}
	switch {
	case started:
	case wm.EnforceStartPoint:
		previewEnforcedBranch(wm, p)
	default:
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

	if cli.PreviewCurrentBlocked(p.fs, p.hubPath) {
		output.Info("[dry-run] Would register '%s' in hop.json", p.branch)
	} else {
		output.Info("[dry-run] Would register '%s' in hop.json and point 'current' at it", p.branch)
	}
	if p.envStart {
		output.Info("[dry-run] Would start environment (when the worktree has one)")
	}
	return res
}

// previewRefusals fails the preview when the real add would refuse to
// create the worktree (see hop.WorktreeManager.CheckAdd), with the real
// run's reason and exit status. The start-point is left unchecked when
// add would fetch first: the fetch may bring it.
func previewRefusals(wm *hop.WorktreeManager, p addPlan) {
	if err := wm.CheckAdd(p.hopspace, p.hubPath, p.branch, p.worktreePath); err != nil {
		refusePreview(p, err)
	}
	if p.fetch {
		return
	}
	if err := wm.CheckStartPoint(p.hopspace, p.hubPath, p.branch, p.startPoint); err != nil {
		refusePreview(p, err)
	}
}

// refusePreview ends the preview of an add the real run would refuse,
// in the form every preview refuses in (refuseDryRun).
func refusePreview(p addPlan, err error) {
	refuseDryRun(fmt.Sprintf("add '%s'", p.branch), err)
}

// previewAddResult is the result add would produce for p, marked dry_run.
// Ports are allocated once the worktree exists, so they are absent; a
// branch add would create has no upstream yet (git sets it on creation);
// env_started is true when add would try to start the environment.
func previewAddResult(g git.GitInterface, p addPlan) addResult {
	res := addResult{
		Branch:     p.branch,
		Path:       p.worktreePath,
		Created:    !localBranchExists(g, p.hubPath, p.branch),
		Task:       p.task,
		EnvStarted: p.envStart,
		DryRun:     true,
	}
	if !res.Created {
		res.Upstream = localBranchUpstream(g, p.hubPath, p.branch)
	}
	// The hub entry add writes: a fresh one carrying the recorded base.
	var entry config.HubBranch
	if base := resolveBranchBase(g, p.hubPath, p.startPoint, p.defaultBranch); base != "" {
		entry.Base = &base
	}
	res.Base = resolveCompareBranch(p.hubConfig, entry)
	return res
}

// localBranchUpstream returns the upstream of local branch in the
// repository at dir in its short form (origin/main), or "" when it tracks
// none.
func localBranchUpstream(g git.GitInterface, dir, branch string) string {
	out, err := g.RunInDir(dir, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// previewGitflowStart reports the git flow start add would run to create
// p.branch, and whether it would run one (it then creates the branch).
func previewGitflowStart(g git.GitInterface, wm *hop.WorktreeManager, p addPlan) (bool, error) {
	base := ""
	if wm.EnforceStartPoint {
		base = gitflowStartBase(g, p.hubPath, p.startPoint, p.defaultBranch)
	}
	gitflow := newGitflowDetector(g, p.hubPath, detector.WithStartBase(base))
	info, err := branchDetectors(afero.NewOsFs(), g, gitflow).DetectBranch(p.branch, p.hubPath)
	if err != nil || info == nil || info.Source != gitflow.Name() {
		return false, err
	}
	if !gitflow.ActionsEnabled() {
		hintGitflowOptIn()
		return false, nil
	}
	if !gitflowStartsNewBranch(g, gitflow, info, p.hubPath, p.branch) {
		return false, nil
	}
	cmd := strings.TrimSpace(fmt.Sprintf("git flow %s start %s %s", info.Type, info.Name, base))
	output.Info("[dry-run] Would run '%s' in a new detached worktree at %s", cmd, displayPath(p.cwd, p.worktreePath))
	return true, nil
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
// preview with the real run's reason and exit status.
func previewEnforcedBranch(wm *hop.WorktreeManager, p addPlan) {
	e, target, err := wm.PreviewExistingBranch(p.hopspace, p.hubPath, p.branch, p.startPoint, p.defaultBranch)
	if err != nil {
		refusePreview(p, err)
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
