package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/afero"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// addCreateError is a refusal of createHopWorktree that ends add with a
// fatal: line. Msg is the line, unprefixed.
type addCreateError struct{ Msg string }

func (e *addCreateError) Error() string { return e.Msg }

// addCreateParams is what createHopWorktree needs: the hub and its
// settings, the branch to add and where it starts, and the choices add
// resolves from flags before creating anything.
type addCreateParams struct {
	FS           afero.Fs
	Git          git.GitInterface
	Docker       *docker.Docker
	Hub          *hop.Hub
	HubPath      string
	Hopspace     *hop.Hopspace
	HopspacePath string
	HookRunner   *hooks.Runner
	// WM carries the start-point enforcement; Detach is set here.
	WM           *hop.WorktreeManager
	GlobalConfig *config.GlobalConfig
	RepoID       string
	Branch       string
	TaskID       string
	// WorktreePath is the path the pre-worktree-add hook gets.
	WorktreePath  string
	StartPoint    string
	Start         addStartPoint
	DefaultBranch string
	// FromFlag is the --from value: it makes git flow start use the
	// resolved start-point as its base.
	FromFlag    string
	CopyIgnored *bool
	MiseMode    string
	EnvStart    bool
	// RefreshRoots restates the hub's worktree set for the shell
	// integration, at the point add always did. A caller that refreshes
	// itself afterwards (remove) leaves it nil.
	RefreshRoots func()
}

// addCreateResult is what a created worktree reports.
type addCreateResult struct {
	Path          string
	BranchExisted bool
	Ports         *config.BranchPorts
	EnvStarted    bool
	EnvTarget     services.EnvTarget
}

// createHopWorktree creates the worktree for p.Branch and registers it in
// the hub, the hopspace and state, as 'git hop add' does: branch-type
// detector, pre-worktree-add hook, worktree, ignored-file copy, mise
// trust, environment and shared-deps set-up, post-worktree-add hook,
// registration, 'current', the roots-cache refresh (when asked), events and
// the environment start.
//
// A refusal comes back as an error: *hop.StateError for a hub whose
// state needs 'doctor --fix', *addCreateError for the rest. Warnings are
// printed as they occur and do not fail the creation.
func createHopWorktree(ctx context.Context, p addCreateParams) (addCreateResult, error) {
	fs, g, hub, hubPath, hopspace := p.FS, p.Git, p.Hub, p.HubPath, p.Hopspace
	branch, worktreePath := p.Branch, p.WorktreePath
	fail := func(format string, a ...interface{}) error {
		return &addCreateError{Msg: fmt.Sprintf(format, a...)}
	}

	// Detect the branch type; its action runs once the worktree exists.
	gitflow := newGitflowDetector(g, hubPath, detector.WithStartBase(
		gitflowStartBase(g, hubPath, explicitStartPoint(p.FromFlag, p.StartPoint), p.DefaultBranch)))
	detectorMgr := branchDetectors(fs, g, gitflow)
	branchInfo, err := detectorMgr.DetectBranch(branch, hubPath)
	if err != nil {
		return addCreateResult{}, fail("Branch type detector failed: %v", err)
	}

	// Execute pre-worktree-add hook with detector env vars
	detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
	addTaskEnv(detectorEnv, p.TaskID)
	if _, err := p.HookRunner.ExecuteHookWithDetector("pre-worktree-add", worktreePath, p.RepoID, branch, detectorEnv); err != nil {
		return addCreateResult{}, fail("Hook pre-worktree-add failed: %v", err)
	}

	// Probed before creation: afterwards the branch exists either way.
	branchExisted := localBranchExists(g, hubPath, branch)

	// A new git-flow branch is created by git flow start, run in its
	// worktree: git-hop creates that worktree detached and leaves the
	// branch to git-flow, so it is created once and the hub, a bare
	// repository git-flow cannot run in, is never its working
	// directory.
	gitflowStarts := gitflowStartsNewBranch(g, gitflow, branchInfo, hubPath, branch)
	p.WM.Detach = gitflowStarts

	// Create Worktree in the current hub
	worktreePath, err = p.WM.CreateWorktreeTransactional(hopspace, hubPath, branch, p.GlobalConfig.Defaults.WorktreeLocation, hub.Config.Repo.Org, hub.Config.Repo.Repo, p.DefaultBranch, p.StartPoint)
	if err != nil {
		if stateErr, ok := err.(*hop.StateError); ok {
			return addCreateResult{}, stateErr
		}
		return addCreateResult{}, fail("Failed to create worktree: %v", err)
	}

	// The detector's add action: git flow start for a new git-flow
	// branch, the opt-in hint when git-flow actions are off. An
	// existing branch has nothing to start.
	if gitflowStarts || !gitflow.StartsBranch(branchInfo) {
		err := detectorMgr.ExecuteAdd(ctx, branchInfo, hubPath, worktreePath)
		if err == nil && gitflowStarts {
			err = checkGitflowStarted(g, worktreePath, branch)
		}
		if err != nil {
			if gitflowStarts {
				undoGitflowWorktree(fs, g, hubPath, worktreePath, branch)
			}
			return addCreateResult{}, fail("Branch type detector failed: %v", err)
		}
	}

	// Seed the new worktree with the ignored local state (.env files,
	// tool config, local caches) of the worktree it was forked from.
	// Runs before the set-up below so any deps-managed path is still
	// absent here and the symlink EnsureDeps creates lands on a clean
	// name. Never fatal — see copyIgnoredIntoWorktree.
	copyIgnoredIntoWorktree(fs, g, hub, p.HopspacePath, worktreePath, p.Start,
		p.GlobalConfig, config.NewGitConfig(), p.CopyIgnored)

	// Trust the new worktree's mise config when its source's is
	// trusted and the same (mise.local.toml may have just been
	// copied), before the set-up and hooks below run mise. Never fatal.
	trustWorktreeMise(execMise{}, p.MiseMode, forkSource(fs, hub, p.Start), worktreePath)

	// Environment and shared deps before post-worktree-add, so the hook
	// finds .env, the compose override and linked deps. Never fatal.
	envTarget := services.EnvTarget{
		Root:         worktreePath,
		Branch:       branch,
		HubPath:      hubPath,
		HopspacePath: p.HopspacePath,
		Hub:          hub.Config,
	}
	setup := services.SetUpWorktree(fs, p.Docker, envTarget, p.GlobalConfig)
	var branchPorts *config.BranchPorts
	if setup.Env != nil {
		branchPorts = setup.Env.Ports
	}

	// Execute post-worktree-add hook
	if _, err := p.HookRunner.ExecuteHookWithDetector("post-worktree-add", worktreePath, p.RepoID, branch, detectorEnv); err != nil {
		output.Warn("Hook post-worktree-add failed: %v", err)
	}

	// Register in Hopspace
	if err := hopspace.RegisterBranch(hubPath, branch, worktreePath); err != nil {
		return addCreateResult{}, fail("Failed to register branch in hopspace: %v", err)
	}

	// Add to Hub
	if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
		return addCreateResult{}, fail("Failed to add branch to hub: %v", err)
	}

	// Record the comparison base when the worktree was forked from a
	// non-default branch. status/list use this for accurate ahead/behind
	// labels (default-branch forks fall through to the hub default).
	// Skip "initial" (root commit, not a branch) and any input that
	// doesn't resolve to a local or remote branch ref (raw SHA, tag).
	if base := resolveBranchBase(g, worktreePath, p.StartPoint, p.DefaultBranch); base != "" {
		if err := hub.SetBranchBase(branch, base); err != nil {
			output.Warn("Failed to record branch base: %v", err)
		}
	}
	recordAddTask(hub, branch, p.TaskID)

	recordAddedWorktree(fs, hub, p.RepoID, hubPath, branch, worktreePath)

	// Update current symlink to point to new worktree
	if err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath); err != nil {
		// Don't fail on symlink error, just warn
		output.Warn("Failed to update current symlink: %v", err)
	}

	// The worktree set just grew. Restate it for the shell integration
	// so a plain cd into the new worktree is detected -- without this
	// the handler stays blind to it for the rest of the session.
	if p.RefreshRoots != nil {
		p.RefreshRoots()
	}

	setup.PublishCreated(ctx, cli.EventBus, hubPath)

	// Last, once the worktree is complete: a failed start only warns.
	// The start is progress beside add's result, so it goes to stderr.
	startTarget := envTarget
	startTarget.Progress = os.Stderr
	envStarted := p.EnvStart && services.StartNewWorktreeEnv(fs, startTarget, p.GlobalConfig, cli.EventBus)

	return addCreateResult{
		Path:          worktreePath,
		BranchExisted: branchExisted,
		Ports:         branchPorts,
		EnvStarted:    envStarted,
		EnvTarget:     envTarget,
	}, nil
}
