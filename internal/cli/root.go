package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/core/upgrade"
	"hop.top/kit/go/core/xdg"
	"hop.top/kit/go/runtime/bus"

	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

var (
	cfgFile        string
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
		ver := fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
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

func Execute() error {
	defer func() {
		if EventBus != nil {
			_ = EventBus.Close(context.Background())
		}
	}()
	return RootCmd.Execute()
}

func init() {
	EventBus = bus.New()

	Root = kitcli.New(kitcli.Config{
		Name:            "git-hop",
		Version:         "dev",
		Short:           "Manage git worktrees and environments",
		DisableValidate: true, // Layer-A annotations not yet adopted; see follow-up track.
		Disable: kitcli.Disable{
			// kit v0.4 registers --config -c (StringArrayP) and --dry-run
			// (Bool) unconditionally; both collide with git-hop's own flags
			// of the same name. Suppress kit's defaults so git-hop keeps
			// its existing single-file --config and per-command --dry-run
			// semantics. NOTE: kit v0.4 has no Disable.Verbose opt-out, so
			// git-hop adopts kit's --verbose -V Count flag instead (was
			// --verbose -v Bool in v0.3). See verboseEnabled() above.
			Config: true,
			DryRun: true,
		},
		Hooks: kitcli.Hooks{
			// Direct assignment to RootCmd.PersistentPreRunE silently
			// overwrites kit's built-in chain (chdir → identity → peer
			// init). The Hooks slot composes additively. Order matters:
			// setupOutputMode initializes output.Verbose via SetupLogger
			// so initConfig's Debug call can actually emit.
			PrePersistentRunE: func(cmd *cobra.Command, args []string) error {
				setupOutputMode()
				initConfig()
				if cmd.Name() != "upgrade" {
					upgrade.NotifyIfAvailable(cmd.Context(), newUpgradeChecker(), os.Stderr)
				}
				return nil
			},
		},
	})

	RootCmd = Root.Cmd
	RootCmd.Version = "dev"

	RootCmd.Long = `git-hop is a context-aware porcelain tool for managing
Git worktrees, Docker environments, and structured workspaces.

Clone Mode:
  git-hop <uri> [path]
  Clones a repository using bare repo + worktree structure (recommended)
  Configure default behavior via global config: bareRepo setting

Worktree Mode:
  git-hop <branch>
  Inside a project root: create/sync worktree for a branch`

	RootCmd.Args = cobra.ArbitraryArgs

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
		globalCfg, err := globalLoader.Load()

		domain := gitDomain
		if domain == "" && err == nil {
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

			useBare := true
			if err == nil {
				useBare = globalCfg.Defaults.BareRepo
			}

			hookOpts := hop.HookMirrorOptions{
				Mode:      hooksMode,
				Overwrite: hooksOverwrite,
				Run:       buildHookMirrorRun(fs, hooksMode, hooksOverwrite),
			}
			dispatch := buildHookDispatch(fs)
			if err := hop.CloneWorktree(fs, g, expandedArg, projectPath, useBare, globalConfig, hookOpts, dispatch); err != nil {
				output.Fatal("Clone failed: %v", err)
			}
			return
		}

		if hubErr == nil {
			branch, exists := hub.Config.Branches[arg]
			if !exists {
				output.Fatal("Worktree '%s' does not exist. Use 'git hop add %s' to create it.", arg, arg)
			}

			worktreePath := branch.Path

			// Capture from-state BEFORE any mutation. A missing or dangling
			// `current` symlink is normal (first hop after a clone), so both
			// fields stay empty and SwitchEnvVars omits them entirely.
			fromBranch, fromWorktreePath := resolveSwitchFromState(fs, hubPath, hub)

			repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

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
			hookRunner := hooks.NewRunner(fs)
			if err := hookRunner.ExecuteHookWithDetector("pre-worktree-switch", worktreePath, repoID, arg, hookEnv); err != nil {
				output.Fatal("Hook pre-worktree-switch failed: %v", err)
			}

			if err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath); err != nil {
				output.Warn("Failed to update current symlink: %v", err)
			}

			if err := os.Chdir(worktreePath); err != nil {
				output.Fatal("Failed to change directory to worktree '%s': %v", worktreePath, err)
			}

			if err := hookRunner.ExecuteHookWithDetector("post-worktree-switch", worktreePath, repoID, arg, hookEnv); err != nil {
				output.Warn("Hook post-worktree-switch failed: %v", err)
			}

			output.Success("Switched to worktree '%s'", arg)
			output.Info("Path: %s", worktreePath)
			return
		}

		output.Fatal("Unknown command or argument: %s", arg)
	}

	pf := RootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "", "config file (default is $XDG_CONFIG_HOME/git-hop/config.json)")
	pf.BoolVar(&jsonOut, "json", false, "output in JSON format")
	pf.BoolVar(&porcelain, "porcelain", false, "machine-readable output")
	// --quiet is already registered by kit/cli.New(); add -q shorthand
	if f := pf.Lookup("quiet"); f != nil {
		f.Shorthand = "q"
	}
	pf.BoolVar(&force, "force", false, "bypass safety checks")
	pf.BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	pf.BoolVarP(&globalConfig, "global", "g", false, "use global hopspace in $GIT_HOP_DATA_HOME (default: local)")

	RootCmd.Flags().StringVar(&gitDomain, "git-domain", "", "Git domain for shorthand notation (e.g., github.com, gitlab.com)")
	RootCmd.Flags().String("branch", "", "branch name for fork-attach mode")
	RootCmd.Flags().StringVar(&hooksMode, "hooks", "", "mirror committed .git-hop/hooks/ on clone: symlink|copy|prompt|none (default: prompt)")
	RootCmd.Flags().BoolVar(&hooksOverwrite, "hooks-overwrite", false, "overwrite an existing hopspace hook with different content (symlink/copy modes)")

	RootCmd.Flags().BoolVar(&adminMode, "admin", false, "")
	RootCmd.Flags().MarkHidden("admin")

	_ = Root.Viper.BindPFlag("json", pf.Lookup("json"))
}

// resolveSwitchFromState reads the hub's `current` symlink and reverse-maps
// its target to a registered branch name. GetCurrentSymlink returns a target
// relative to the hub, so it is joined against hubPath and made absolute
// before comparison — the same resolve-and-compare idiom the move command
// uses.
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
		abs, err := filepath.Abs(b.Path)
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

func initConfig() {
	v := Root.Viper
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		configDir, err := xdg.ConfigDir("git-hop")
		if err != nil {
			configDir = filepath.Join(os.Getenv("HOME"), ".config", "git-hop")
		}
		v.AddConfigPath(configDir)
		v.SetConfigName("config")
		v.SetConfigType("json")
	}

	v.SetEnvPrefix("GIT_HOP")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err == nil && verboseEnabled() {
		output.Debug("using config file: %s", v.ConfigFileUsed())
	}
}

// buildHookMirrorRun returns a closure that resolves the hooks install
// mode (flag → env → git config → default "prompt") and invokes
// hooks.MirrorCommittedHooks against the freshly-cloned worktree.
//
// Lives here (not in internal/hop) because internal/hooks already imports
// internal/hop; flipping the dependency would create an import cycle.
func buildHookMirrorRun(fs afero.Fs, flagMode string, overwrite bool) func(string, string) error {
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
			Mode:         mode,
			Overwrite:    overwrite,
			Stdout:       os.Stdout,
			Stderr:       os.Stderr,
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
			fmt.Fprintf(os.Stderr,
				"hooks: installed=%d skipped=%d already-present=%d warned=%d\n",
				res.Installed, res.Skipped, res.AlreadyPresent, res.Warned)
		}
		return nil
	}
}

// buildHookDispatch returns the clone lifecycle-hook dispatch callbacks,
// each closing over a hooks.Runner.
//
// Lives here for the same reason as buildHookMirrorRun: internal/hooks
// already imports internal/hop, so internal/hop cannot call the hook
// runner directly without creating an import cycle. The caller injects.
func buildHookDispatch(fs afero.Fs) hop.HookDispatchOptions {
	runner := hooks.NewRunner(fs)
	dispatchTo := func(hookName string) func(string, string, string) error {
		return func(path, repoID, branch string) error {
			return runner.ExecuteHook(hookName, path, repoID, branch)
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

func setupOutputMode() {
	quiet = Root.Viper.GetBool("quiet")

	var mode output.Mode
	if jsonOut {
		mode = output.ModeJSON
	} else if porcelain {
		mode = output.ModePorcelain
	} else if quiet {
		mode = output.ModeQuiet
	} else {
		mode = output.ModeHuman
	}

	output.SetViper(Root.Viper)
	output.SetupLogger(mode, verboseEnabled())
}
