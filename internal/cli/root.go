package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
	kitout "hop.top/kit/go/console/output"
	"hop.top/kit/go/core/upgrade"
	"hop.top/kit/go/core/xdg"
	"hop.top/kit/go/runtime/bus"

	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/shell"
)

var (
	jsonOut        bool
	porcelain      bool
	quiet          bool
	force          bool
	dryRun         bool
	gitDomain      string
	globalConfig   bool
	adminMode      bool
	hooksMode      string
	hooksOverwrite bool

	version string
)

// verboseEnabled reports whether kit's --verbose count flag was raised at
// least once (-V, -VV, ...). Reads from kit's viper key "verbose"; safe to
// call before flag parsing (returns false). Replaces the v0.3-era boolean
// package var that collided with kit v0.4's default --verbose -V Count flag.
func verboseEnabled() bool {
	if Root == nil || Root.Viper == nil {
		return false
	}
	return Root.Viper.GetInt("verbose") > 0
}

var Root *kitcli.Root

// RootCmd is the cobra root command — preserved for backward compat
// with cmd/*.go init() AddCommand calls.
var RootCmd *cobra.Command

// EventBus is the application-wide event bus. Initialized during
// root setup; available to all commands via this package-level var.
var EventBus bus.Bus

func SetVersion(v, c, d string) {
	version = v
	if Root != nil {
		// kit's version template prints "v<version>"; strip a leading "v"
		// here (git-hop's build injects one via `git describe`) so the
		// rendered string reads "v1.2.3", not "vv1.2.3". Mirrors the
		// same normalization kit's own cli.New does for Config.Version.
		ver := fmt.Sprintf("%s (commit: %s, built: %s)", strings.TrimPrefix(v, "v"), c, d)
		Root.Config.Version = ver
		if RootCmd != nil {
			RootCmd.Version = ver
		}
	}
}

func IsURI(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@") || strings.HasSuffix(s, ".git")
}

// ExpandShorthand turns an `org/repo` clone shorthand into a full SSH URI.
// Anything already URI-shaped, or not exactly two slash-separated segments,
// is returned verbatim.
//
// This is the no-context fallback: it decides purely on the shape of the
// string, so an `org/repo`-shaped branch name is indistinguishable from a
// clone shorthand here. Callers that DO have context — i.e. that are inside
// a hub and can see which worktrees actually exist — must go through
// ResolveArg instead, which consults the hub before falling back to this.
func ExpandShorthand(s string, gitDomain string) string {
	if IsURI(s) {
		return s
	}

	parts := strings.Split(s, "/")
	if len(parts) == 2 && !strings.Contains(s, " ") {
		if gitDomain == "" {
			gitDomain = "github.com"
		}
		return fmt.Sprintf("git@%s:%s.git", gitDomain, s)
	}

	return s
}

// ResolveArg decides whether the bare positional argument to `git hop` names
// an existing worktree (switch mode) or a repository to clone (clone mode).
//
// knownBranches is the hub's registered branch set, or nil when the caller is
// not inside a hub. A registered branch wins outright, whatever its name looks
// like: the hub is ground truth about which worktrees exist, so `feature/login`
// resolves to a switch when that worktree is registered and to a clone
// shorthand when it is not. Only when the hub has nothing by that name — or
// there is no hub at all — does the shape-based ExpandShorthand fallback run.
//
// This replaces an allowlist of conventional-commit-ish branch prefixes
// (feat, fix, bug, ...) that guessed at which `a/b` strings were branch names.
// The guess mis-routed every prefix nobody enumerated — `feature/`, `release/`,
// `hotfix/`, personal prefixes like `jad/` — into clone mode.
func ResolveArg(arg string, gitDomain string, knownBranches map[string]config.HubBranch) string {
	if _, exists := knownBranches[arg]; exists {
		return arg
	}
	return ExpandShorthand(arg, gitDomain)
}

// Execute runs the command line in os.Args, reporting a usage error on
// stderr (see reportUsageError). Map the returned error to the process
// status with ExitCode.
func Execute() error {
	defer func() {
		if EventBus != nil {
			_ = EventBus.Close(context.Background())
		}
	}()
	installUsageErrors(RootCmd)
	args := os.Args[1:]
	err := checkUnknownSubcommand(RootCmd, args)
	if err == nil {
		err = RootCmd.Execute()
	}
	var ue *UsageError
	if errors.As(err, &ue) {
		reportUsageError(RootCmd.ErrOrStderr(), ue, args)
	}
	return err
}

// reportUsageError prints ue as git does: an `error:` line, then the
// usage of the command reached.
//
// When machine output was asked for, the usage block is left out: it is
// prose for a person at a terminal. In JSON mode the error is the JSON
// record an operation failure emits, so a consumer parses one shape
// whichever way the command failed.
func reportUsageError(w io.Writer, ue *UsageError, args []string) {
	cmd := ue.Cmd
	if cmd == nil {
		cmd = RootCmd
	}
	if jsonUsageErrorRequested(cmd, args) {
		output.ErrorJSON(w, ue.Err.Error())
		return
	}
	fmt.Fprintf(w, "error: %s\n", ue.Err)
	if !structuredOutputRequested(cmd, args) {
		fmt.Fprint(w, usageBlock(cmd))
	}
}

func init() {
	EventBus = bus.New()

	Root = kitcli.New(kitcli.Config{
		Name:            "git-hop",
		Version:         "dev",
		Short:           "Manage git worktrees and environments",
		DisableValidate: true, // Layer-A annotations not yet adopted; see follow-up track.
		Disable: kitcli.Disable{
			// kit registers --dry-run (Bool) unconditionally; it collides
			// with git-hop's own -n/--dry-run and its per-command support
			// guard. Suppress kit's so git-hop keeps those semantics.
			// kit's -c/--config is adopted as is; initConfig consumes it.
			// NOTE: kit v0.4 has no Disable.Verbose opt-out, so git-hop
			// adopts kit's --verbose -V Count flag instead (was --verbose
			// -v Bool in v0.3). See verboseEnabled() above.
			DryRun: true,
		},
		Hooks: kitcli.Hooks{
			// Direct assignment to RootCmd.PersistentPreRunE silently
			// overwrites kit's built-in chain (chdir → identity → peer
			// init). The Hooks slot composes additively. Order matters:
			// setupOutputMode initializes output.Verbose via SetupLogger
			// so initConfig's Debug call can actually emit.
			PrePersistentRunE: func(cmd *cobra.Command, args []string) error {
				setupOutputMode(cmd)
				if err := checkDryRunSupported(cmd); err != nil {
					output.FatalCode(exitUsage, "%s", err)
				}
				if err := initConfig(); err != nil {
					return asUsageError(cmd, err)
				}
				attachEventSinks(cmd)
				if cmd.Name() != "upgrade" {
					upgrade.NotifyIfAvailable(cmd.Context(), newUpgradeChecker(), os.Stderr)
				}
				return nil
			},
		},
	})

	RootCmd = Root.Cmd
	RootCmd.Version = "dev"
	installHelpCommand(RootCmd)

	RootCmd.Long = `git-hop is a context-aware porcelain tool for managing
Git worktrees, Docker environments, and structured workspaces.

Clone Mode:
  git-hop <uri> [path]
  Clones a repository into a bare hub + worktree structure

Worktree Mode:
  git-hop <branch>
  Inside a project root: create/sync worktree for a branch`

	RootCmd.Args = cobra.ArbitraryArgs
	SupportDryRun(RootCmd)

	RootCmd.Run = func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if adminMode {
				printAdminHelp(cmd)
				os.Exit(0)
			}
			cmd.Help()
			os.Exit(0)
		}

		arg := args[0]
		cwd, _ := os.Getwd()
		fs := afero.NewOsFs()
		g := git.New()

		globalLoader := config.NewGlobalLoader()
		globalCfg := globalLoader.Load()

		domain := gitDomain
		if domain == "" {
			domain = globalCfg.Defaults.GitDomain
		}
		if domain == "" {
			domain = "github.com"
		}

		// Hub lookup is hoisted above shorthand expansion so the expansion can
		// see which worktrees actually exist. FindHub/LoadHub are pure reads
		// (afero stat walk + JSON unmarshal), so running them before the
		// clone branch costs nothing and mutates nothing. A missing hub is the
		// normal out-of-hub case, not an error: hub stays nil and ResolveArg
		// degrades to the shape-based fallback.
		hubPath, hubErr := hop.FindHub(fs, cwd)
		var hub *hop.Hub
		if hubErr == nil {
			var loadErr error
			hub, loadErr = hop.LoadHub(fs, hubPath)
			if loadErr != nil {
				output.Fatal("Failed to load hub config: %v", loadErr)
			}
		}

		var knownBranches map[string]config.HubBranch
		if hub != nil {
			knownBranches = hub.Config.Branches
		}

		expandedArg := ResolveArg(arg, domain, knownBranches)

		if IsURI(expandedArg) {
			if dryRun {
				RejectDryRun("clone")
			}
			branch, _ := cmd.Flags().GetString("branch")

			if branch != "" && hubErr == nil {
				if err := hop.ForkAttach(fs, g, expandedArg, branch, hubPath); err != nil {
					output.Fatal("Fork-Attach failed: %v", err)
				}
				return
			}

			projectPath := ""
			if len(args) > 1 {
				projectPath = args[1]
			}

			startEnv := DecideAutoEnvStart(NegatableFlag(cmd, "env-start", cloneEnvStartFlag, cloneNoEnvStartFlag), globalCfg)

			hookOpts := hop.HookMirrorOptions{
				Mode:      hooksMode,
				Overwrite: hooksOverwrite,
				Run:       buildHookMirrorRun(fs, hooksMode, hooksOverwrite, expandedArg),
			}
			dispatch := BuildHookDispatch(fs, expandedArg)
			dispatch.SetUpEnv = func(hubPath string) { setUpClonedWorktree(fs, hubPath, globalCfg) }
			if err := hop.CloneWorktree(fs, g, expandedArg, projectPath, globalConfig, hookOpts, dispatch); err != nil {
				output.Fatal("Clone failed: %v", err)
			}

			// A clone produces the user's very first worktree, and until it
			// reaches the roots cache the shell integration has nothing to
			// match $PWD against -- which is what made the whole chdir
			// feature read as inert on a fresh install. The refresh lives
			// here rather than inside CloneWorktree because internal/shell
			// imports internal/hop; the reverse import would cycle.
			clonedHub := cloneHubPath(expandedArg, projectPath)
			refreshRootsCacheAt(fs, clonedHub)
			if startEnv {
				startClonedEnv(fs, clonedHub, globalCfg)
			}
			return
		}

		if hubErr == nil {
			branch, exists := hub.Config.Branches[arg]
			if !exists {
				output.Fatal("Worktree '%s' does not exist. Use 'git hop add %s' to create it.", arg, arg)
			}

			worktreePath := resolveSwitchWorktreePath(branch, hubPath)

			// Capture from-state BEFORE any mutation. A missing or dangling
			// `current` symlink is normal (first hop after a clone), so both
			// fields stay empty and SwitchEnvVars omits them entirely.
			fromBranch, fromWorktreePath := resolveSwitchFromState(fs, hubPath, hub)

			repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

			if dryRun {
				previewSwitch(fs, hub.Config.Repo.URI, repoID, arg, worktreePath)
				return
			}

			detectorMgr := detector.NewManager(fs, g)
			detectorMgr.Register(detector.NewGitFlowNextDetector(g))
			detectorMgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))
			branchInfo, err := detectorMgr.DetectBranch(arg, hubPath)
			if err != nil {
				output.Fatal("Branch type detector failed: %v", err)
			}
			hookEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
			for k, v := range hooks.SwitchEnvVars(fromBranch, fromWorktreePath, hooks.TriggerHop) {
				hookEnv[k] = v
			}

			// A non-zero pre-worktree-switch aborts before the symlink write.
			// The symlink is the load-bearing step: os.Chdir below only moves
			// this process, while the shell wrapper navigates by resolving
			// `current` after the binary exits.
			hookRunner := hooks.NewRunner(fs).ForRepo(hub.Config.Repo.URI)
			if _, err := hookRunner.ExecuteHookWithDetector("pre-worktree-switch", worktreePath, repoID, arg, hookEnv); err != nil {
				output.Fatal("Hook pre-worktree-switch failed: %v", err)
			}

			if err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath); err != nil {
				output.Warn("Failed to update current symlink: %v", err)
			}

			if err := os.Chdir(worktreePath); err != nil {
				output.Fatal("Failed to change directory to worktree '%s': %v", worktreePath, err)
			}

			postResult, err := hookRunner.ExecuteHookWithDetector("post-worktree-switch", worktreePath, repoID, arg, hookEnv)
			if err != nil {
				output.Warn("Hook post-worktree-switch failed: %v", err)
			}

			// Refresh the shell integration's worktree-path cache. That
			// cache is what lets the chdir handler answer "is $PWD a
			// worktree?" without forking on every prompt, and this is the
			// natural place to keep it warm: the binary is already running
			// and already holds the hub's branch set. Best-effort -- a
			// cache write must never fail a switch.
			if err := shell.MergeRootsCache(fs, hub, hubPath); err != nil {
				output.Debug("failed to refresh worktree roots cache: %v", err)
			}

			// Emit worktree.switched event. Published next to, but
			// independent of, hook dispatch: a failing post-hook only
			// warns above, and a bus error never fails the switch.
			publishWorktreeSwitched(EventBus, hub, hubPath, arg, worktreePath)

			output.Success("Switched to worktree '%s'", arg)
			output.Info("Path: %s", worktreePath)

			// The hook navigated the user itself. The switch SUCCEEDED --
			// symlink written, event published, success reported above --
			// so this is not an error path. Re-raising the hook's status as
			// git-hop's own is the only way the signal survives: the shell
			// wrapper reads `$?` and nothing else, and it decides whether to
			// cd only after this process is gone.
			if postResult.NavigationHandled {
				os.Exit(hooks.ExitNavigationHandled)
			}
			return
		}

		output.Fatal("Unknown command or argument: %s", arg)
	}

	pf := RootCmd.PersistentFlags()
	// --config -c is kit's (see initConfig). kit hides it from --help as
	// plumbing; git-hop has always listed it, so keep it visible.
	if f := pf.Lookup("config"); f != nil {
		f.Hidden = false
		f.Usage = "config file (default is $XDG_CONFIG_HOME/git-hop/config.json) " +
			"or key=value override (repeatable)"
	}
	pf.BoolVar(&jsonOut, "json", false, "output in JSON format")
	pf.BoolVar(&porcelain, "porcelain", false, "machine-readable output")
	// --quiet is already registered by kit/cli.New(); add -q shorthand
	if f := pf.Lookup("quiet"); f != nil {
		f.Shorthand = "q"
	}
	pf.BoolVar(&force, "force", false, "bypass safety checks")
	pf.BoolVarP(&dryRun, "dry-run", "n", false, "preview changes without applying")
	pf.BoolVarP(&globalConfig, "global", "g", false, "use global hopspace in $GIT_HOP_DATA_HOME (default: local)")

	RootCmd.Flags().StringVar(&gitDomain, "git-domain", "", "Git domain for shorthand notation (e.g., github.com, gitlab.com)")
	RootCmd.Flags().String("branch", "", "branch name for fork-attach mode")
	RootCmd.Flags().StringVar(&hooksMode, "hooks", "", "mirror committed .git-hop/hooks/ on clone: symlink|copy|prompt|none (default: prompt)")
	RootCmd.Flags().BoolVar(&hooksOverwrite, "hooks-overwrite", false, "overwrite an existing hopspace hook with different content (symlink/copy modes)")
	AddEnvStartFlags(RootCmd.Flags(), &cloneEnvStartFlag, &cloneNoEnvStartFlag)

	RootCmd.Flags().BoolVar(&adminMode, "admin", false, "")
	RootCmd.Flags().MarkHidden("admin")

	_ = Root.Viper.BindPFlag("json", pf.Lookup("json"))
}

// cloneHubPath reproduces the project root CloneWorktree derived, so the
// caller can find the hub the clone just wrote without CloneWorktree having
// to report it.
//
// Duplicating the derivation is deliberate. The alternative -- widening
// CloneWorktree's signature to return the path -- changes a function the
// clone tests, the init path, and the e2e suite all pin, to serve a
// best-effort cache refresh. The rule being mirrored is one line long and
// stated in one place (repo name = last URI segment minus ".git", rooted at
// cwd unless the user named a path), and a drift here degrades to a cache
// that is not refreshed, never to a bad clone.
func cloneHubPath(uri, projectPath string) string {
	root := projectPath
	if root == "" {
		parts := strings.Split(uri, "/")
		name := strings.TrimSuffix(parts[len(parts)-1], ".git")
		cwd, _ := os.Getwd()
		root = filepath.Join(cwd, name)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

// refreshRootsCacheAt restates the worktree set of the hub at hubPath in
// the shell integration's roots cache.
//
// The hub-by-path form exists for callers that have just written a hub to
// disk rather than holding one in memory -- clone being the case that
// matters. Everything about it is best-effort: a hub that cannot be found
// or loaded means there is nothing to restate, which on this path is
// indistinguishable from a clone that did not produce one, and either way
// the operation it trails has already succeeded and must not be failed by a
// cache write.
func refreshRootsCacheAt(fs afero.Fs, hubPath string) {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Debug("failed to load hub for worktree roots cache: %v", err)
		return
	}
	if err := shell.RebuildRootsCache(fs, hub, hubPath); err != nil {
		output.Debug("failed to refresh worktree roots cache: %v", err)
	}
}

// publishWorktreeSwitched emits events.WorktreeSwitched after a successful
// branch switch, mirroring the publish shape the move/remove/merge commands
// use. HopspacePath is the hopspace the hub resolves to, as in every other
// worktree event; resolving it reads the hub config only, no I/O.
//
// Errors are swallowed and a nil bus is tolerated: the event is an
// observability side channel, never a gate on the switch itself.
func publishWorktreeSwitched(b bus.Bus, hub *hop.Hub, hubPath, branch, worktreePath string) {
	if b == nil {
		return
	}
	hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
	_ = b.Publish(context.Background(), bus.NewEvent(
		events.WorktreeSwitched, events.Source,
		events.WorktreeEvent{
			Path:         worktreePath,
			Branch:       branch,
			HopspacePath: hopspacePath,
			RepoPath:     hubPath,
		},
	))
}

// resolveSwitchWorktreePath turns a registered branch's recorded path into the
// absolute worktree path the switch drives everything off: the `current`
// symlink write, os.Chdir, and GIT_HOP_WORKTREE_PATH in the switch hooks.
//
// hop.json normally stores a path relative to the hub ("hops/main"), so it must
// be anchored on the hub rather than the process cwd — otherwise hopping from a
// subdirectory computes a worktree path that does not exist. Absolute recorded
// paths pass through untouched.
func resolveSwitchWorktreePath(branch config.HubBranch, hubPath string) string {
	return config.ResolveWorktreePath(branch.Path, hubPath)
}

// resolveSwitchFromState reads the hub's `current` symlink and reverse-maps
// its target to a registered branch name. GetCurrentSymlink returns a target
// relative to the hub, so it is joined against hubPath and made absolute
// before comparison — the same resolve-and-compare idiom the move command
// uses. Registered branch paths go through the same hub-anchored resolution,
// keeping both sides of the comparison independent of the process cwd.
//
// Returns two empty strings when `current` is absent or dangling. That is the
// expected state on the first hop after a clone and is never an error: hook
// env simply carries no from-state.
func resolveSwitchFromState(fs afero.Fs, hubPath string, hub *hop.Hub) (fromBranch string, fromWorktreePath string) {
	target, err := hop.GetCurrentSymlink(fs, hubPath)
	if err != nil {
		return "", ""
	}

	absTarget, err := filepath.Abs(filepath.Join(hubPath, target))
	if err != nil {
		return "", ""
	}

	for name, b := range hub.Config.Branches {
		abs, err := filepath.Abs(config.ResolveWorktreePath(b.Path, hubPath))
		if err != nil {
			continue
		}
		if abs == absTarget {
			return name, absTarget
		}
	}

	// Symlink resolves outside the registered branch set (dangling or stale
	// entry): report the path, leave the branch unknown.
	return "", absTarget
}

func printAdminHelp(cmd *cobra.Command) {
	fmt.Println("Admin commands:")
	fmt.Println()
	for _, sub := range cmd.Commands() {
		if sub.Hidden && sub.Name() != "" {
			fmt.Printf("  %-20s %s\n", sub.Name(), sub.Short)
		}
	}
}

// initConfig loads Root.Viper's config from kit's -c/--config tokens.
//
// A bare path names a config file to read in place of the default
// $XDG_CONFIG_HOME/git-hop/config.json; repeated paths layer in order. A
// key=value token overrides one setting above every file. kit rejects a
// path that does not exist, and that is reported as a bad flag value.
func initConfig() error {
	paths, overrides, err := Root.ConfigArgs()
	if err != nil {
		return fmt.Errorf("invalid -c/--config flag: %w", err)
	}
	configDir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config", "git-hop")
	}
	loadConfig(Root.Viper, paths, overrides, configDir)
	return nil
}

// loadConfig reads paths into v, or config.json under defaultDir when
// paths is empty, then merges overrides on top. The environment
// (GIT_HOP_*) and flags still win over both, per viper's precedence.
// A file that cannot be read is skipped, as it always has been.
func loadConfig(v *viper.Viper, paths []string, overrides map[string]any, defaultDir string) {
	v.SetEnvPrefix("GIT_HOP")
	v.AutomaticEnv()

	read := func(merge bool) {
		readIn := v.ReadInConfig
		if merge {
			readIn = v.MergeInConfig
		}
		if err := readIn(); err == nil && verboseEnabled() {
			output.Debug("using config file: %s", v.ConfigFileUsed())
		}
	}
	if len(paths) == 0 {
		v.AddConfigPath(defaultDir)
		v.SetConfigName("config")
		v.SetConfigType("json")
		read(false)
	}
	for i, p := range paths {
		v.SetConfigFile(p)
		read(i > 0)
	}

	if len(overrides) > 0 {
		_ = v.MergeConfigMap(overrides)
	}
}

// buildHookMirrorRun returns a closure that resolves the hooks install
// mode (flag → env → git config → default "prompt") and invokes
// hooks.MirrorCommittedHooks against the freshly-cloned worktree. uri is
// the clone's origin URL.
//
// Lives here (not in internal/hop) because internal/hooks already imports
// internal/hop; flipping the dependency would create an import cycle.
func buildHookMirrorRun(fs afero.Fs, flagMode string, overwrite bool, uri string) func(string, string) error {
	return func(worktreePath, repoID string) error {
		envMode := os.Getenv("GIT_HOP_HOOKS")
		var configured string
		if gc := config.NewGitConfig(); gc != nil {
			configured = gc.GetStringOrDefault(config.KeyHooksInstallMode)
		}
		mode := hooks.ResolveMode(flagMode, envMode, configured)

		mopts := hooks.MirrorOpts{
			WorktreePath: worktreePath,
			RepoID:       repoID,
			RepoURI:      uri,
			Mode:         mode,
			Overwrite:    overwrite,
		}
		// Only attach Stdin in TTY interactive contexts; the install
		// helper degrades prompt → none when Stdin is nil.
		if mode == hooks.ModePrompt && isStdinTTY() {
			mopts.Stdin = os.Stdin
		}

		res, err := hooks.MirrorCommittedHooks(fs, mopts)
		if err != nil {
			return err
		}
		if res.Installed > 0 || res.Warned > 0 || res.Skipped > 0 || res.AlreadyPresent > 0 {
			output.Note("hooks: installed=%d skipped=%d already-present=%d warned=%d",
				res.Installed, res.Skipped, res.AlreadyPresent, res.Warned)
		}
		return nil
	}
}

// BuildHookDispatch returns the clone lifecycle-hook dispatch callbacks,
// each closing over a hooks.Runner. `git hop init` reuses PostWorktreeAdd
// for the initial worktree it creates, so both commands hand hooks the
// same path, repo ID, branch and environment.
//
// Lives here for the same reason as buildHookMirrorRun: internal/hooks
// already imports internal/hop, so internal/hop cannot call the hook
// runner directly without creating an import cycle. The caller injects.
// uri is the repository's origin URL (see hooks.Runner.ForRepo).
func BuildHookDispatch(fs afero.Fs, uri string) hop.HookDispatchOptions {
	runner := hooks.NewRunner(fs).ForRepo(uri)
	dispatchTo := func(hookName string) func(string, string, string) error {
		return func(path, repoID, branch string) error {
			_, err := runner.ExecuteHook(hookName, path, repoID, branch)
			return err
		}
	}
	return hop.HookDispatchOptions{
		PreClone:        dispatchTo("pre-clone"),
		PostWorktreeAdd: dispatchTo("post-worktree-add"),
		PostClone:       dispatchTo("post-clone"),
	}
}

// isStdinTTY reports whether os.Stdin is a terminal. Used to decide whether
// prompt mode should actually prompt (vs degrade to none).
func isStdinTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func setupOutputMode(cmd *cobra.Command) {
	quiet = Root.Viper.GetBool("quiet")

	req := outputRequest{
		json:      jsonOut,
		porcelain: porcelain,
		quiet:     quiet,
		format:    Root.Viper.GetString("format"),
	}
	if f := cmd.Flags().Lookup("format"); f != nil {
		req.formatExplicit = f.Changed
	}

	format, formatOpts, err := req.resultFormat(declaresResult(cmd))
	if err != nil {
		output.FatalCode(129, "%v", err)
	}
	if format != "" {
		if err := output.ValidateResultCols(cmd, Root.Viper); err != nil {
			output.FatalCode(129, "%v", err)
		}
	}
	output.SetResultFormat(format, formatOpts...)

	output.SetViper(Root.Viper)
	output.SetupLogger(req.mode(format), verboseEnabled())
	output.SetQuiet(req.quiet)
	output.SetupColor(colorWhen(cmd, Root.Viper.GetBool("no-color")))
}

// colorWhen is the colour setting for a run of cmd: never under the
// global --no-color, else the command's own --color=<when> when given,
// else auto. --color is a per-command flag (repair) rather than a global
// one, so a command without it runs on auto.
func colorWhen(cmd *cobra.Command, noColor bool) output.ColorWhen {
	if noColor {
		return output.ColorNever
	}
	if f := cmd.Flags().Lookup("color"); f != nil && f.Changed {
		if w, ok := f.Value.(*output.ColorWhen); ok {
			return *w
		}
	}
	return output.ColorAuto
}

// outputRequest is what a command line asked of the output layer. The
// pre-run reads it from the parsed flags; a usage error, which stops
// the parse, reads it from the raw arguments (see requestedOutput).
type outputRequest struct {
	json, porcelain, quiet bool
	format                 string
	formatExplicit         bool
}

// declaresResult reports whether cmd declares an output schema (kit's
// SetOutputSchema): only those commands have a result to render.
func declaresResult(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	_, _, ok := kitcli.GetOutputSchemaJSON(cmd)
	return ok
}

// resultFormat decides which structured format, if any, a command
// renders its result in. It returns "" for the human view.
//
// Only commands that declare a result take part. For the others the
// output flags keep their previous meaning -- --json and --porcelain only
// switch the logger mode -- until each is given a result of its own.
//
// --json is --format json. --porcelain is kit's text format in its
// lines style: one tab-separated record per line, no header, columns in
// the result's declared order. The returned format and options are what
// output.EmitResult hands kit's Dispatch; the root viper is left as parsed.
//
// Every rejection happens here, in the pre-run, so a contradictory or
// unknown mode fails before the command mutates anything.
func (r outputRequest) resultFormat(declared bool) (format string, formatOpts []string, err error) {
	if !declared {
		return "", nil, nil
	}

	switch {
	case r.json && r.porcelain:
		return "", nil, fmt.Errorf("--json and --porcelain are mutually exclusive")
	case r.json:
		if r.formatExplicit && r.format != kitout.JSON {
			return "", nil, fmt.Errorf("--json and --format=%s are mutually exclusive", r.format)
		}
		return kitout.JSON, nil, nil
	case r.porcelain:
		if r.formatExplicit {
			return "", nil, fmt.Errorf("--porcelain and --format=%s are mutually exclusive", r.format)
		}
		return kitout.Text, []string{"style=lines"}, nil
	}

	if output.IsHumanFormat(r.format) {
		return "", nil, nil
	}
	if _, ok := kitout.Default.Lookup(r.format); !ok {
		return "", nil, fmt.Errorf("unknown output format %q (valid: %s)",
			r.format, strings.Join(kitout.Default.Keys(), ", "))
	}
	return r.format, nil, nil
}

// mode is the logger mode for a command rendering its result in format,
// as returned by resultFormat.
func (r outputRequest) mode(format string) output.Mode {
	switch {
	case format == kitout.JSON || (format == "" && r.json):
		return output.ModeJSON
	case format != "" || r.porcelain:
		return output.ModePorcelain
	case r.quiet:
		return output.ModeQuiet
	default:
		return output.ModeHuman
	}
}
