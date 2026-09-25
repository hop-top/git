package cmd

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/services"
)

// addFromFlag holds the --from CLI flag value.
var addFromFlag string

// addCopyIgnoredFlag / addNoCopyIgnoredFlag hold the two halves of the
// --[no-]copy-ignored pair. Each is only consulted when explicitly set;
// with neither given, hop.add.copyIgnored decides.
var (
	addCopyIgnoredFlag   bool
	addNoCopyIgnoredFlag bool
)

var addCmd = &cobra.Command{
	Use:     "add [branch]",
	Aliases: []string{"create", "new"},
	Short:   "Add a new worktree and environment",
	Long: `Add a new worktree and environment for a branch.

Creates the branch (if missing) from the start-point, checks it out under
hops/<branch>, and sets up shared dependencies.

The environment is not started unless asked: --env-start starts it once
the worktree exists, through the same code as 'git hop env start'. 'git
config hop.env.autoStart true' (or GIT_HOP_AUTO_ENV_START=true) makes that
the default; --no-env-start skips it for one run. A worktree without an
environment is skipped, and a failed start only warns: add still succeeds.

An existing branch is checked out as-is unless --from is given. With
--from it must end up at that start-point: it is fast-forwarded when at or
behind it, and add refuses when it is ahead or has diverged, so no local
commit is lost.

With --task <id>, the task id is recorded as branches.<branch>.task in
hop.json and exported to the add hooks as GIT_HOP_TASK. The id is never
part of the branch name. Without a <branch>, one is derived from the task
as <type>/<slug> (type from its type:<x> tag, slug from its title) via
'tlc task show <id> --format json'. A branch that exists only as
origin/<branch> is checked out tracking it.

The new worktree is also seeded with the git-ignored local files (.env,
tool config, small caches) present in the worktree it forks from. Nothing
is overwritten, dependency directories are left to the deps layer, and
entries over hop.add.copyIgnoredMaxSize (default 10m) are skipped and
reported. Disable with --no-copy-ignored or 'git config hop.add.copyIgnored
false'.

To keep one ignored path out of that copy, put #-hop-# on a comment line
directly above its pattern in any ignore file:

    #-hop-#
    .tlc/

The marker must be on its own comment line: git reads a mid-line '#' as
part of the pattern.

Before resolving the start-point, add runs 'git fetch origin' so a new
branch does not start from a stale origin/<branch>. By default it does so
only when the start-point is an origin ref: the default branch or an
explicit origin/<branch>. 'git config hop.add.fetch true|false' makes that
a standing choice; --fetch / --no-fetch decide for one run. When the
fetch was asked for (--fetch, hop.add.fetch true) a failure is fatal; the
automatic one only warns and carries on from the refs already present.
A hub with no origin remote has nothing to fetch: a requested fetch is
skipped with a hint.

With --dry-run, add reports the fetch, branch, start-point, worktree path,
hooks and environment start it would run, then stops: nothing is fetched,
created or written and no hook runs.`,
	Args: addArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		g := git.New()
		d := docker.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// Find the hub by searching up the directory tree
		hubPath, err := hop.FindHub(fs, cwd)
		if err != nil {
			output.Fatal("Not in a git-hop hub. Please run 'git hop <uri>' to clone first, or initialize a hub.")
		}

		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			output.Fatal("Failed to load hub: %v", err)
		}

		hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
		hopspace, err := hop.LoadHopspace(fs, hopspacePath)
		if err != nil {
			output.Fatal("Failed to load hopspace at %s: %v", hopspacePath, err)
		}

		// Load global config for worktree location
		globalLoader := config.NewGlobalLoader()
		globalConfig := globalLoader.Load()

		branch, taskID := mustResolveAddTarget(args)

		// Calculate worktree path (needed for pre-worktree-add hook)
		dataHome := hop.GetGitHopDataHome()
		ctx := hop.WorktreeLocationContext{
			HubPath:  hubPath,
			Branch:   branch,
			Org:      hub.Config.Repo.Org,
			Repo:     hub.Config.Repo.Repo,
			DataHome: dataHome,
			URI:      hub.Config.Repo.URI,
		}
		worktreePath := hop.ExpandWorktreeLocation(globalConfig.Defaults.WorktreeLocation, ctx)
		worktreePath = filepath.Clean(worktreePath)

		repoID := repoid.For(hubPath, hub.Config.Repo)

		// Resolve the branch start-point per precedence:
		// --from (CLI) > GIT_HOP_ADD_FROM env > hop.add.defaultStartPoint > built-in default ("default-branch").
		startPoint := resolveAddStartPoint(addFromFlag, os.Getenv("GIT_HOP_ADD_FROM"), globalConfig.Defaults.DefaultStartPoint)

		hookRunner := hooks.NewRunner(fs).ForRepo(hub.Config.Repo.URI, hubPath)

		wm := hop.NewWorktreeManager(fs, g)
		wm.EnforceStartPoint = addFromFlag != ""

		fetch := decideFetch(g, config.NewGitConfig(),
			cli.NegatableFlag(cmd, "fetch", addFetchFlag, addNoFetchFlag),
			hubPath, startPoint, hub.Config.Repo.DefaultBranch)
		hintNoOrigin(fetch)
		envStart := cli.DecideAutoEnvStart(
			cli.NegatableFlag(cmd, "env-start", addEnvStartFlag, addNoEnvStartFlag), globalConfig)

		// Everything below writes; the preview must stop before any of it.
		if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
			previewAdd(g, wm, hookRunner, addPlan{
				cwd:           cwd,
				hubPath:       hubPath,
				hopspace:      hopspace,
				fetch:         fetch.fetches(),
				repoID:        repoID,
				branch:        branch,
				worktreePath:  worktreePath,
				startPoint:    startPoint,
				defaultBranch: hub.Config.Repo.DefaultBranch,
				task:          taskID,
				envStart:      envStart,
			})
			return
		}

		output.Info("Adding branch %s...", branch)

		if fetch.fetches() {
			fetchOrigin(g, hubPath, fetch)
		}

		// Create detector manager and register detectors
		detectorMgr := newBranchDetectors(fs, g, hubPath)

		// Execute pre-add (detector OnAdd)
		detectorCtx := context.Background()
		branchInfo, err := detectorMgr.ExecutePreAdd(detectorCtx, branch, hubPath, worktreePath)
		if err != nil {
			output.Fatal("Branch type detector failed: %v", err)
		}

		// Execute pre-worktree-add hook with detector env vars
		detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
		addTaskEnv(detectorEnv, taskID)
		if _, err := hookRunner.ExecuteHookWithDetector("pre-worktree-add", worktreePath, repoID, branch, detectorEnv); err != nil {
			output.Fatal("Hook pre-worktree-add failed: %v", err)
		}

		// Probed before creation: afterwards the branch exists either way.
		branchExisted := localBranchExists(g, hubPath, branch)

		// Create Worktree in the current hub
		worktreePath, err = wm.CreateWorktreeTransactional(hopspace, hubPath, branch, globalConfig.Defaults.WorktreeLocation, hub.Config.Repo.Org, hub.Config.Repo.Repo, hub.Config.Repo.DefaultBranch, startPoint)
		if err != nil {
			// Check if it's a state error
			if stateErr, ok := err.(*hop.StateError); ok {
				output.Error("Cannot create worktree due to state issues:")
				output.Error("  %s at %s: %s", stateErr.Type, stateErr.Path, stateErr.Message)
				output.Info("\nRun 'git hop doctor --fix' to resolve these issues")
				os.Exit(1)
			}
			output.Fatal("Failed to create worktree: %v", err)
		}

		// Seed the new worktree with the ignored local state (.env files,
		// tool config, local caches) of the worktree it was forked from.
		// Runs before the set-up below so any deps-managed path is still
		// absent here and the symlink EnsureDeps creates lands on a clean
		// name. Never fatal — see copyIgnoredIntoWorktree.
		copyIgnoredIntoWorktree(fs, g, hub, hopspacePath, worktreePath, startPoint,
			globalConfig, config.NewGitConfig(),
			copyIgnoredOverride(cmd, addCopyIgnoredFlag, addNoCopyIgnoredFlag))

		// Environment and shared deps before post-worktree-add, so the hook
		// finds .env, the compose override and linked deps. Never fatal.
		envTarget := services.EnvTarget{
			Root:         worktreePath,
			Branch:       branch,
			HubPath:      hubPath,
			HopspacePath: hopspacePath,
			Hub:          hub.Config,
		}
		setup := services.SetUpWorktree(fs, d, envTarget, globalConfig)
		var branchPorts *config.BranchPorts
		if setup.Env != nil {
			branchPorts = setup.Env.Ports
		}

		// Execute post-worktree-add hook
		if _, err := hookRunner.ExecuteHookWithDetector("post-worktree-add", worktreePath, repoID, branch, detectorEnv); err != nil {
			output.Warn("Hook post-worktree-add failed: %v", err)
		}

		// Register in Hopspace
		if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
			output.Fatal("Failed to register branch in hopspace: %v", err)
		}

		// Add to Hub
		if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
			output.Fatal("Failed to add branch to hub: %v", err)
		}

		// Record the comparison base when the worktree was forked from a
		// non-default branch. status/list use this for accurate ahead/behind
		// labels (default-branch forks fall through to the hub default).
		// Skip "initial" (root commit, not a branch) and any input that
		// doesn't resolve to a local or remote branch ref (raw SHA, tag).
		if base := resolveBranchBase(g, worktreePath, startPoint, hub.Config.Repo.DefaultBranch); base != "" {
			if err := hub.SetBranchBase(branch, base); err != nil {
				output.Warn("Failed to record branch base: %v", err)
			}
		}
		recordAddTask(hub, branch, taskID)

		recordAddedWorktree(fs, hub, repoID, hubPath, branch, worktreePath)

		// Update current symlink to point to new worktree
		if err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath); err != nil {
			// Don't fail on symlink error, just warn
			output.Warn("Failed to update current symlink: %v", err)
		}

		// The worktree set just grew. Restate it for the shell integration
		// so a plain cd into the new worktree is detected -- without this
		// the handler stays blind to it for the rest of the session.
		refreshRootsCache(fs, hub, hubPath)

		setup.PublishCreated(context.Background(), cli.EventBus, hubPath)

		// Last, once the worktree is complete: a failed start only warns.
		// The start is progress beside add's result, so it goes to stderr.
		startTarget := envTarget
		startTarget.Progress = os.Stderr
		envStarted := envStart && services.StartNewWorktreeEnv(fs, startTarget, globalConfig, cli.EventBus)

		if output.IsStructured() {
			emitResult(cmd, newAddResult(g, hub, branch, worktreePath, !branchExisted, branchPorts, envStarted))
			return
		}

		output.Info("Created hopspace for '%s'", branch)

		output.Info("Worktree: %s", displayPath(cwd, worktreePath))

		// If running inside an AI coding agent, hint how to add the worktree directory
		if hint := agentDirHint(worktreePath); hint != "" {
			output.Info("Run: %s", hint)
		} else if os.Getenv("OPENCODE") == "1" {
			opencodeAgentHint(worktreePath, os.Stdin)
		}

		if branchPorts != nil && len(branchPorts.Ports) > 0 {
			var minPort, maxPort int
			var servicesList []string
			first := true
			for svc, p := range branchPorts.Ports {
				if first || p < minPort {
					minPort = p
				}
				if first || p > maxPort {
					maxPort = p
				}
				first = false
				servicesList = append(servicesList, svc)
			}
			sort.Strings(servicesList)

			output.Info("Ports: %d-%d", minPort, maxPort)
			output.Info("Services: %s", strings.Join(servicesList, ", "))
		}
	},
}

func init() {
	cli.RootCmd.AddCommand(addCmd)
	addCmd.Flags().StringVar(&addFromFlag, "from", "",
		"start-point: branch name, ref, SHA, or 'initial' for the root commit; an existing branch is fast-forwarded to it")
	// pflag has no auto-negation, so both halves of the --[no-]copy-ignored
	// pair are registered explicitly (same shape as --no-prompt /
	// --no-verify elsewhere). Neither flag set leaves the decision to
	// hop.add.copyIgnored.
	addCmd.Flags().BoolVar(&addCopyIgnoredFlag, "copy-ignored", true,
		"copy git-ignored files from the source worktree into the new one")
	addCmd.Flags().BoolVar(&addNoCopyIgnoredFlag, "no-copy-ignored", false,
		"do not copy git-ignored files into the new worktree")
	addCmd.Flags().BoolVar(&addFetchFlag, "fetch", false,
		"fetch origin before resolving the start-point (default: when it is an origin ref)")
	addCmd.Flags().BoolVar(&addNoFetchFlag, "no-fetch", false,
		"do not fetch origin before resolving the start-point")
	cli.AddEnvStartFlags(addCmd.Flags(), &addEnvStartFlag, &addNoEnvStartFlag)
	addCmd.Flags().StringVar(&addTaskFlag, "task", "",
		"record a task id for the worktree (metadata only); without <branch>, derive the branch from the task via tlc")
	addCmd.ValidArgsFunction = completeRemoteBranchNames
	declareOutputSchema(addCmd, &addResult{})
}
