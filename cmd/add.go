package cmd

import (
	"context"
	"os"
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

The start-point is the first of --from, GIT_HOP_ADD_FROM and 'git config
hop.add.defaultStartPoint' that is set. Each takes a ref or SHA, or one
of these keywords:

    cwd             the branch checked out in the worktree add runs in;
                    outside every worktree (the hub root), as
                    default-branch. The built-in default.
    default-branch  the local default branch (repo.defaultBranch in
                    hop.json, else the branch origin/HEAD names);
                    origin/<default> when there is no local one
    origin-default  origin/<default>, the local branch when origin has
                    none; the default before cwd
    initial         the root commit

Inside a worktree with a detached HEAD, cwd is the commit it points at,
as with 'git switch -c'; no base branch is recorded for the new branch.

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

Tool-state directories (.tlc/, .hop/) are never copied, at any depth,
marker or not. 'git config --add hop.add.copyIgnoredExclude <pattern>'
(repeatable) leaves more out: a name matches at any depth, a pattern with
a '/' matches from the worktree root, '*' and '?' glob. A leading '!'
removes a pattern, built-in ones included: '!.tlc' copies .tlc/ again.

Before resolving the start-point, add runs 'git fetch origin' so a new
branch does not start from a stale origin/<branch>. By default it does so
only when the start-point is an origin ref: origin-default, an explicit
origin/<branch>, or default-branch when there is no local default
branch. 'git config hop.add.fetch true|false' makes that a standing
choice; --fetch / --no-fetch decide for one run. When the
fetch was asked for (--fetch, hop.add.fetch true) a failure is fatal; the
automatic one only warns and carries on from the refs already present.
A hub with no origin remote has nothing to fetch: a requested fetch is
skipped with a hint.

mise is optional. When it is on PATH and the new worktree has mise
config, add trusts that config ('mise trust <worktree>') once the
ignored files are copied, provided the source worktree's is trusted and
the new worktree has exactly its config files, byte for byte, as mise's
own listing names them. Otherwise it trusts nothing and says why in a
hint (an untrusted source, a config that differs), with the command to
trust it by hand. Without mise, or without mise config, add says
nothing about it. 'git config hop.add.miseTrust always' trusts it
whenever it has mise config, 'never' leaves mise alone; --mise-trust /
--no-mise-trust decide for one run.

add never deletes what it finds at the worktree path: a file or a
non-empty directory there is refused, and an empty directory is reused,
as 'git worktree add' does.

With --dry-run, add reports the resolved start-point and the setting
that picked it, then the fetch, branch, worktree path, mise trust, hooks
and environment start it would run, and stops: nothing is fetched,
created or written and no hook runs. An add the real run would refuse
(the worktree exists, a file or non-empty directory is in its way, the
branch name is invalid or checked out in another worktree, the
start-point is not a commit, --from cannot move the existing branch)
fails with exit status 1 and "[dry-run] would refuse to add '<branch>':"
before the real run's reason.

--dry-run also lists the ignored entries the run will copy and those it
will skip, each with its reason (built-in default, hop.add.copyIgnoredExclude,
#-hop-# marker, size limit, deps layer). A normal run prints only a count;
-V names the entries.`,
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
		worktreePath := expandAddWorktreePath(hub, hubPath, branch, globalConfig)

		repoID := repoid.For(hubPath, hub.Config.Repo)

		// Resolve the branch start-point per precedence:
		// --from (CLI) > GIT_HOP_ADD_FROM env > hop.add.defaultStartPoint
		// (default cwd: the branch of the worktree add runs in, else the
		// local default branch).
		defaultBranch := effectiveDefaultBranch(g, hubPath, hub.Config.Repo.DefaultBranch)
		spValue, spOrigin := pickAddStartPoint(addFromFlag, os.Getenv("GIT_HOP_ADD_FROM"), globalConfig.Defaults.DefaultStartPoint)
		start, err := resolveAddStartPoint(g, hubPath, cwd, spValue, spOrigin, defaultBranch)
		if err != nil {
			output.Fatal("%v", err)
		}
		startPoint := start.Spec
		output.Debug("start-point: %s", start.Describe(defaultBranch))

		hookRunner := hooks.NewRunner(fs).ForRepo(hub.Config.Repo.URI, hubPath)

		wm := hop.NewWorktreeManager(fs, g)
		wm.EnforceStartPoint = addFromFlag != ""

		fetch := decideFetch(g, config.NewGitConfig(),
			cli.NegatableFlag(cmd, "fetch", addFetchFlag, addNoFetchFlag),
			hubPath, startPoint, defaultBranch)
		hintNoOrigin(fetch)
		miseMode := miseTrustMode(config.NewGitConfig(),
			cli.NegatableFlag(cmd, "mise-trust", addMiseTrustFlag, addNoMiseTrustFlag))
		envStart := cli.DecideAutoEnvStart(
			cli.NegatableFlag(cmd, "env-start", addEnvStartFlag, addNoEnvStartFlag), globalConfig)

		// Everything below writes; the preview must stop before any of it.
		if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
			res := previewAdd(g, wm, hookRunner, addPlan{
				fs:            fs,
				cwd:           cwd,
				hubPath:       hubPath,
				hopspace:      hopspace,
				hubConfig:     hub.Config,
				fetch:         fetch.fetches(),
				repoID:        repoID,
				branch:        branch,
				worktreePath:  worktreePath,
				startPoint:    startPoint,
				startWhy:      start,
				defaultBranch: defaultBranch,
				task:          taskID,
				envStart:      envStart,
				miseTrust:     miseMode,
				miseSource:    forkSource(fs, hub, start),
				copyPreview: func() {
					previewCopyIgnored(fs, g, hub, hopspacePath, worktreePath, start, globalConfig,
						config.NewGitConfig(), copyIgnoredOverride(cmd, addCopyIgnoredFlag, addNoCopyIgnoredFlag))
				},
			})
			if output.IsStructured() {
				emitResult(cmd, res)
			}
			return
		}

		output.Info("Adding branch %s...", branch)

		if fetch.fetches() {
			fetchOrigin(g, hubPath, fetch)
		}

		created, err := createHopWorktree(context.Background(), addCreateParams{
			FS:            fs,
			Git:           g,
			Docker:        d,
			Hub:           hub,
			HubPath:       hubPath,
			Hopspace:      hopspace,
			HopspacePath:  hopspacePath,
			HookRunner:    hookRunner,
			WM:            wm,
			GlobalConfig:  globalConfig,
			RepoID:        repoID,
			Branch:        branch,
			TaskID:        taskID,
			WorktreePath:  worktreePath,
			StartPoint:    startPoint,
			Start:         start,
			DefaultBranch: defaultBranch,
			FromFlag:      addFromFlag,
			CopyIgnored:   copyIgnoredOverride(cmd, addCopyIgnoredFlag, addNoCopyIgnoredFlag),
			MiseMode:      miseMode,
			EnvStart:      envStart,
			RefreshRoots:  func() { refreshRootsCache(fs, hub, hubPath) },
		})
		if err != nil {
			// Check if it's a state error
			if stateErr, ok := err.(*hop.StateError); ok {
				output.Error("Cannot create worktree due to state issues:")
				output.Error("  %s at %s: %s", stateErr.Type, stateErr.Path, stateErr.Message)
				output.Info("\nRun 'git hop doctor --fix' to resolve these issues")
				os.Exit(1)
			}
			output.Fatal("%s", err.Error())
		}
		worktreePath = created.Path
		branchPorts, envStarted, branchExisted := created.Ports, created.EnvStarted, created.BranchExisted

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
		"start-point: ref, SHA, or cwd|default-branch|origin-default|initial (default: hop.add.defaultStartPoint, else cwd); an existing branch is fast-forwarded to it")
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
	addCmd.Flags().BoolVar(&addMiseTrustFlag, "mise-trust", false,
		"trust the new worktree's mise config whenever it has one (default: hop.add.miseTrust, auto)")
	addCmd.Flags().BoolVar(&addNoMiseTrustFlag, "no-mise-trust", false,
		"do not trust the new worktree's mise config")
	cli.AddEnvStartFlags(addCmd.Flags(), &addEnvStartFlag, &addNoEnvStartFlag)
	addCmd.Flags().StringVar(&addTaskFlag, "task", "",
		"record a task id for the worktree (metadata only); without <branch>, derive the branch from the task via tlc")
	addCmd.ValidArgsFunction = completeRemoteBranchNames
	declareOutputSchema(addCmd, &addResult{})
}

// explicitStartPoint is the start-point git flow start gets as its
// [base]: the resolved one when --from named it, none otherwise (the
// branch type picks its own).
func explicitStartPoint(fromFlag, resolved string) string {
	if fromFlag == "" {
		return ""
	}
	return resolved
}
