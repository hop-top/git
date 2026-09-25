package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/shell"
	"hop.top/kit/go/runtime/bus"
)

var (
	forceFlag          bool
	dryRunFlag         bool
	keepBackupFlag     bool
	keepBackupFlagSet  bool // --keep-backup typed, either way
	regularFlag        bool
	restorePath        string
	noHooksFlag        bool
	noPromptFlag       bool
	enableChdirFlag    bool
	initHooksMode      string
	initHooksOverwrite bool
	// initRunFlags is the running command's flag set, for the dry run's
	// closing hint (initCmd itself cannot be named from convertRepo).
	initRunFlags *pflag.FlagSet
	// initRunCmd is the running command, which renders init's result.
	initRunCmd *cobra.Command
)

func init() {
	cli.RootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:     "init",
	Args:    cobra.NoArgs,
	Aliases: []string{"setup", "install"},
	Short:   "Initialize git-hop repository structure",
	Long: `Initialize git-hop repository structure with interactive setup for worktree conversion.

Converting a standard repository prompts for a structure. Scripts and
other non-interactive callers should pass --no-prompt, which selects the
recommended bare repo + worktrees conversion without asking; add
--regular for a regular repo + worktrees instead. Without --no-prompt, a
run with nothing readable on stdin fails rather than waiting for an
answer that will never come.

Committed .git-hop/hooks/ scripts are mirrored into the user's hopspace
when init succeeds; control via --hooks (symlink|copy|prompt|none).
See docs/hooks.md for details.`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		g := git.New()
		keepBackupFlagSet = cmd.Flags().Changed("keep-backup")
		initRunFlags = cmd.Flags()
		initRunCmd = cmd

		if restorePath != "" {
			handleRestore(fs, g, restorePath, forceFlag, dryRunFlag)
			return
		}

		cwd, err := os.Getwd()
		if err != nil {
			output.Error("Failed to get current directory: %v", err)
			os.Exit(1)
		}

		structure := hop.DetectRepoStructure(fs, g, cwd)
		cwd, structure = resolveInitTarget(fs, g, cwd, structure)
		if structure == config.NotGit {
			output.Error("Not in a git repository")
			os.Exit(1)
		}

		if structure == config.BareWorktreeRoot || structure == config.WorktreeRoot || structure == config.WorktreeChild {
			handleAlreadyInitializedWithFlags(afero.NewOsFs(), g, cwd, structure, noHooksFlag, enableChdirFlag)
			return
		}

		if structure != config.StandardRepo {
			output.Error("Repository structure not supported for conversion: %s", structure)
			os.Exit(1)
		}

		showConversionMenu(fs, g, cwd)
	},
}

func showConversionMenu(fs afero.Fs, g git.GitInterface, repoPath string) {
	out := output.ReportOut()
	fmt.Fprintln(out, `
-----------------------------------------------------
  Git-Hop Repository Structure
-----------------------------------------------------

Current repository: Standard git repository`)

	fmt.Fprintf(out, "Location: %s\n", repoPath)

	remoteURL, err := g.GetRemoteURL(repoPath)
	if err == nil {
		fmt.Fprintf(out, "Remote: origin (%s)\n", remoteURL)
	} else {
		fmt.Fprintln(out, "Remote: none")
	}

	branch, err := g.GetCurrentBranch(repoPath)
	if err == nil && branch != "" {
		fmt.Fprintf(out, "Branch: %s\n", branch)
	}

	fmt.Fprintln(out, "Structure Options:")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  1. Convert to bare repo + worktrees (Recommended)")
	fmt.Fprintln(out, "     Creates bare .git repo + worktree directories")
	fmt.Fprintln(out, "     Preserves all your work and branches")
	fmt.Fprintln(out, "     Backup created automatically")
	fmt.Fprintln(out, "     Follows: Git worktree best practices")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  2. Convert to regular repo + worktrees")
	fmt.Fprintln(out, "     Same worktree structure as option 1")
	fmt.Fprintln(out, "     But allows commits in repo root (not recommended)")
	fmt.Fprintln(out, "     Use if: You need repo root to be working tree")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  3. Register as-is (Limited)")
	fmt.Fprintln(out, "     Uses current repository structure without changes")
	fmt.Fprintln(out, "     Manual worktree management required")
	fmt.Fprintln(out, "     Some git-hop features limited")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  q. Quit")
	fmt.Fprintln(out, "")

	choice, err := resolveInitChoice(noPromptFlag, regularFlag)
	if err != nil {
		// An unanswerable prompt is a failed precondition, not a
		// cancellation: exit non-zero naming the flag that lets a
		// non-interactive caller convert without being asked.
		output.FatalCode(exitPromptUnanswerable, "%s", err.Error())
	}

	switch choice {
	case choiceBareWorktree:
		convertRepo(fs, g, repoPath, true, false, noHooksFlag, enableChdirFlag)
	case choiceRegularWorktree:
		convertRepo(fs, g, repoPath, false, false, noHooksFlag, enableChdirFlag)
	case choiceRegisterAsIs:
		registerAsIs(fs, g, repoPath, noHooksFlag, enableChdirFlag)
	case choiceQuit:
		fmt.Fprintln(out, "Cancelled")
		// Return rather than os.Exit so the root command's deferred
		// EventBus.Close() still runs. A user quitting is a clean exit.
		return
	}
}

func convertRepo(fs afero.Fs, g git.GitInterface, repoPath string, useBare, isRegular, noHooks, enableChdir bool) {
	if useBare {
		refuseOperationInProgress(fs, repoPath)
	}
	refuseDetachedHead(fs, g, repoPath)
	var linked *hop.LinkedCarryPlan
	if useBare {
		linked = planInitLinkedCarry(fs, g, repoPath)
	}

	converter := hop.NewConverter(fs, g)
	converter.DryRun = dryRunFlag
	converter.Force = forceFlag
	// Backup settings are read from the repository being converted, so
	// its local config applies as well as the global one.
	gc := config.NewGitConfigIn(repoPath)
	converter.KeepBackup = resolveInitKeepBackup(keepBackupFlag, keepBackupFlagSet, gc)
	backupRoot, err := conversionBackupRoot(gc)
	if err != nil {
		output.Error("%v", err)
		os.Exit(1)
	}
	converter.BackupRoot = backupRoot

	if !dryRunFlag && !forceFlag {
		status, _ := g.RunInDir(repoPath, "git", hop.StatusPorcelainArgs(linked, repoPath)...)
		if status != "" {
			output.Error("Repository has uncommitted changes")
			output.Hint(`Please commit or stash changes before converting:
  git commit -m "WIP: Save work"
  # OR
  git stash push -m "WIP: Save work"

Then run: %s

To convert anyway, carrying uncommitted changes into the new worktree,
staged and unstaged as they are:
  %s`, initProceedCommand(initRunFlags), initForceCommand(initRunFlags))
			os.Exit(1)
		}
	}

	if dryRunFlag {
		if output.IsStructured() {
			branch, _ := g.GetCurrentBranch(repoPath)
			res := initConversionResult(repoPath, branch, initWorktreePath(repoPath, branch, useBare), useBare)
			res.DryRun = true
			res.BackupKept = converter.KeepBackup
			res.Registered = true
			emitResult(initRunCmd, res)
			return
		}
		fmt.Println(initDryRunBanner)
		fmt.Printf("Repository: %s\n", repoPath)

		fmt.Printf("Remote: %s\n", initRemoteLabel(g, repoPath))

		branch, _ := g.GetCurrentBranch(repoPath)
		fmt.Printf("Branch: %s\n", branch)

		// Without optional locks: status would otherwise refresh the index.
		status, _ := g.RunInDir(repoPath, "git", append([]string{"--no-optional-locks"}, hop.StatusPorcelainArgs(linked, repoPath)...)...)
		if status == "" {
			fmt.Println("Status: clean")
		} else {
			fmt.Println("Status: dirty")
		}

		fmt.Println("\nConversion plan:")
		for _, step := range initConversionPlan(backupRoot, branch, useBare, linked) {
			fmt.Println(step)
		}
		previewLocalConfig(g, repoPath, branch, useBare)
		fmt.Println(initSetUpStep(branch))

		if !noHooks {
			previewInitWorktreeAdd(fs, g, repoPath, branch, useBare)
		}
		previewInitFinish(fs, g, initWorktreePath(repoPath, branch, useBare), repoPath, repoPath, noHooks, enableChdir)

		output.Hint("To proceed with conversion, run:\n  %s", initProceedCommand(initRunFlags))
		return
	}

	output.Info("Converting repository...")

	result, err := converter.ConvertToBareWorktree(repoPath, useBare, true)
	cwdErr := reanchorInitCwd(repoPath)
	if err != nil {
		output.Error("Conversion failed: %v", err)

		for _, errMsg := range result.Errors {
			fmt.Fprintf(output.DiagOut(), "  - %s\n", errMsg)
		}
		for _, warning := range result.Warnings {
			output.Warn("%s", warning)
		}
		if errors.Is(err, hop.ErrLinkedWorktrees) {
			output.Hint("To convert to the regular layout, which keeps .git in place:\n  %s",
				initRegularCommand(initRunFlags))
		}
		// A failed conversion keeps its backup: it is what the automatic
		// rollback restored from.
		reportPreservedBackup(output.DiagOut(), fs, result.BackupPath)

		os.Exit(1)
	}
	if cwdErr != nil {
		output.Warn("could not change into %s after conversion: %v", repoPath, cwdErr)
	}

	// Load hub config to get actual worktree path
	hub, err := hop.LoadHub(fs, repoPath)
	var mainWorktreePath string
	var currentBranchName string
	var isRegularRepo bool

	if err == nil {
		// The default branch's worktree: hop.json also lists the linked
		// worktrees a bare conversion carried.
		currentBranchName = hub.Config.Repo.DefaultBranch
		if branch, ok := hub.Config.Branches[currentBranchName]; ok {
			if branch.Path == "." {
				// Regular repo - current branch is in repo root
				mainWorktreePath = repoPath
				isRegularRepo = true
			} else {
				mainWorktreePath = config.ResolveWorktreePath(branch.Path, repoPath)
				isRegularRepo = false
			}
		}
	}

	// Update current symlink to point to the main worktree
	if mainWorktreePath != "" && !isRegularRepo {
		if err := hop.UpdateCurrentSymlink(fs, repoPath, mainWorktreePath); err != nil {
			output.Warn("failed to create current symlink: %v", err)
		}
	}

	registered := registerConvertedHub(fs, hub, repoPath, mainWorktreePath, currentBranchName, isRegularRepo)

	// Emit hopspace.initialized after successful conversion, then
	// worktree.created for the worktree the conversion created, as clone
	// does for its initial worktree.
	if hub != nil {
		_ = cli.EventBus.Publish(context.Background(), bus.NewEvent(
			events.HopspaceInitialized, events.Source,
			events.HopspaceEvent{
				Path: repoPath,
				Org:  hub.Config.Repo.Org,
				Repo: hub.Config.Repo.Repo,
			},
		))
		if mainWorktreePath != "" {
			created := services.WorktreeSetup{Target: services.EnvTarget{
				Root:         mainWorktreePath,
				Branch:       currentBranchName,
				HubPath:      repoPath,
				HopspacePath: hop.ResolveHopspacePath(repoPath, hub.Config.Repo),
				Hub:          hub.Config,
			}}
			created.PublishCreated(context.Background(), cli.EventBus, repoPath)
		}
	}

	out := output.ReportOut()
	fmt.Fprintln(out, "\nConversion successful!")
	if isRegularRepo {
		fmt.Fprintf(out, "Project structure:\n")
		fmt.Fprintf(out, "  %s/\n", repoPath)
		fmt.Fprintf(out, "    .git/              (repository)\n")
		fmt.Fprintf(out, "    hop.json\n")
		fmt.Fprintf(out, "    worktrees/         (future branch worktrees)\n")
		fmt.Fprintf(out, "    (repo root is %s branch working tree)\n", currentBranchName)
	} else {
		fmt.Fprintf(out, "Project structure:\n")
		fmt.Fprintf(out, "  %s/  (bare repository)\n", repoPath)
		fmt.Fprintf(out, "    hop.json\n")
		fmt.Fprintf(out, "    hops/\n")
		fmt.Fprintf(out, "      %s/              (worktree for %s branch)\n", currentBranchName, currentBranchName)
		fmt.Fprintf(out, "    current -> hops/%s  (symlink)\n", currentBranchName)
	}
	printCarriedWorktrees(result.Carried)

	for _, warning := range result.Warnings {
		output.Warn("%s", warning)
	}
	for _, hint := range result.Hints {
		output.Hint("%s", hint)
	}

	reportPreservedBackup(out, fs, result.BackupPath)

	if !noHooks {
		if err := installInitHooks(fs, repoPath, mainWorktreePath, isRegularRepo); err != nil {
			output.Warn("failed to install hooks directory: %v", err)
		} else {
			hookInstallPath := repoPath
			if mainWorktreePath != "" && !isRegularRepo {
				hookInstallPath = mainWorktreePath
			}
			fmt.Fprintf(out, "\nHooks directory created: %s/.git-hop/hooks/\n", hookInstallPath)
			printInitHooksHint(repoPath, hookInstallPath != repoPath)
		}
	}

	// Mirror any committed .git-hop/hooks/ into the user's hopspace.
	hookInstallPath := repoPath
	if mainWorktreePath != "" && !isRegularRepo {
		hookInstallPath = mainWorktreePath
	}
	mirrorInitHooks(fs, g, hookInstallPath, repoPath, initHooksMode, initHooksOverwrite, noHooks)

	// Environment and shared deps before post-worktree-add, as add and
	// clone do. Not a hook: runs under --no-hooks too.
	setUpInitWorktree(fs, hub, repoPath, mainWorktreePath, currentBranchName)

	// After the mirror, as clone does: a committed hook then applies to
	// the worktree that carried it. The initial worktree is hops/<branch>
	// for a bare conversion and the repo root for a regular one.
	// --no-hooks turns dispatch off along with the hooks dir and mirror.
	if mainWorktreePath != "" && !noHooks {
		dispatchInitWorktreeAdd(fs, g, repoPath, mainWorktreePath, currentBranchName)
	}

	if enableChdir {
		if err := maybeInstallShellIntegration(fs, true); err != nil {
			output.Warn("failed to install shell integration: %v", err)
		}
	}

	if output.IsStructured() {
		res := initConversionResult(repoPath, currentBranchName, mainWorktreePath, !isRegularRepo)
		res.Backup = result.BackupPath
		res.BackupKept = backupOnDisk(fs, result.BackupPath)
		res.Registered = registered
		emitResult(initRunCmd, res)
		return
	}

	next := "You can now:\n"
	if !isRegularRepo {
		next += fmt.Sprintf("  cd %s   # Work on %s branch\n", mainWorktreePath, currentBranchName)
	}
	output.Hint("%s", next+initNextSteps)
}

// resolveInitKeepBackup decides whether the conversion backup outlives a
// successful conversion. A typed --keep-backup or --keep-backup=false is
// a decision about this run and wins; otherwise hop.backup.keepBackup,
// read from the repository being converted (so local and global config
// both apply), supplies the default, falling back to false.
func resolveInitKeepBackup(flagValue, flagSet bool, gc *config.GitConfig) bool {
	if flagSet {
		return flagValue
	}
	if gc == nil {
		return false
	}
	return gc.GetBoolOrDefault(config.KeyBackupKeepBackup)
}

// reportPreservedBackup tells the user where the conversion backup is,
// but only while it is actually on disk. The converter deletes it after a
// successful conversion unless --keep-backup; a failed conversion or a
// failed cleanup leaves it behind. Asking the filesystem keeps this line
// honest in every one of those cases.
func reportPreservedBackup(w io.Writer, fs afero.Fs, backupPath string) {
	if !backupOnDisk(fs, backupPath) {
		return
	}
	fmt.Fprintf(w, "\nBackup preserved at: %s\n", backupPath)
	output.Hint("%s", restoreHint(backupPath))
	output.Hint("To remove backup manually:\n  rm -rf %s", backupPath)
}

// backupOnDisk reports whether the conversion backup at backupPath is
// still there.
func backupOnDisk(fs afero.Fs, backupPath string) bool {
	if backupPath == "" {
		return false
	}
	exists, _ := afero.DirExists(fs, backupPath)
	return exists
}

// initNextSteps is the command list init's closing hint suggests.
const initNextSteps = `  git hop add <branch>       # Add new branch
  git hop <branch>           # Jump to worktree
  git hop                    # List all worktrees`

func registerAsIs(fs afero.Fs, g git.GitInterface, repoPath string, noHooks, enableChdir bool) {
	refuseDetachedHead(fs, g, repoPath)
	if !dryRunFlag {
		output.Info("Registering repository as-is...")
	}

	remoteURL, err := g.GetRemoteURL(repoPath)
	var org, repo string

	if err != nil {
		// No remote configured - use local path
		output.Info("No remote configured - using local path for registration")
		absPath, err := filepath.Abs(repoPath)
		if err != nil {
			output.Error("Failed to get absolute path: %v", err)
			os.Exit(1)
		}
		repo = filepath.Base(absPath)
		org = filepath.Base(filepath.Dir(absPath))
	} else {
		org, repo = hop.ParseRepoFromURL(remoteURL)
		if org == "" || repo == "" {
			output.Error("Could not parse org/repo from URL")
			os.Exit(1)
		}
	}

	branch, err := g.GetCurrentBranch(repoPath)
	if err != nil {
		output.Error("Failed to get current branch: %v", err)
		os.Exit(1)
	}

	if dryRunFlag {
		previewRegisterAsIs(fs, g, org, repo, branch, repoPath, noHooks, enableChdir)
		return
	}

	repoKey := org + "/" + repo
	registerAsIsHub(fs, remoteURL, org, repo, branch, repoPath)

	// Emit hopspace.initialized for register-as-is path.
	_ = cli.EventBus.Publish(context.Background(), bus.NewEvent(
		events.HopspaceInitialized, events.Source,
		events.HopspaceEvent{
			Path: repoPath,
			Org:  org,
			Repo: repo,
		},
	))

	fmt.Println("Repository registered successfully!")
	fmt.Printf("  Repo: %s\n", repoKey)
	fmt.Printf("  Branch: %s\n", branch)
	fmt.Printf("  Path: %s\n", repoPath)

	if remoteURL == "" {
		output.Hint("Repository has no remote configured.")
	}
	output.Hint("Some git-hop features are limited with this structure.\n"+
		"Consider converting to worktree structure for full functionality:\n"+
		"  %s", initConvertCommand(initRunFlags))

	if !noHooks {
		if err := installInitHooks(fs, repoPath, "", false); err != nil {
			output.Warn("failed to install hooks directory: %v", err)
		} else {
			fmt.Printf("\nHooks directory created: %s/.git-hop/hooks/\n", repoPath)
			printInitHooksHint(repoPath, false)
		}
	}

	// Mirror any committed .git-hop/hooks/ into the user's hopspace.
	mirrorInitHooks(fs, g, repoPath, repoPath, initHooksMode, initHooksOverwrite, noHooks)

	if enableChdir {
		if err := maybeInstallShellIntegration(fs, true); err != nil {
			output.Warn("failed to install shell integration: %v", err)
		}
	}
}

// handleAlreadyInitialized wraps handleAlreadyInitializedWithFlags using global flag values.
func handleAlreadyInitialized(fs afero.Fs, g git.GitInterface, path string, structure config.StructureType) {
	handleAlreadyInitializedWithFlags(fs, g, path, structure, noHooksFlag, enableChdirFlag)
}

// handleAlreadyInitializedWithFlags is called when git hop init is run in a repo that is
// already using the worktree structure. It ensures hooks are installed (unless --no-hooks)
// and prints a summary so the command is idempotent.
//
// Before printing the "already initialized" summary, it back-fills a
// missing hop.json from runtime git state — needed for legacy bare-
// worktree repos cloned/created outside `git hop` (or where hop.json
// was lost). Without the back-fill, those repos report "already
// initialized" and yet downstream commands (status, list, add) treat
// the directory as an un-registered hub. See cmd/init_backfill.go.
//
// A dry run reports all of that and does none of it.
func handleAlreadyInitializedWithFlags(fs afero.Fs, g git.GitInterface, path string, structure config.StructureType, noHooks, enableChdir bool) {
	if dryRunFlag {
		if output.IsStructured() {
			emitResult(initRunCmd, previewAlreadyInitializedResult(fs, g, path, structure))
			return
		}
		previewAlreadyInitialized(fs, g, path, structure, noHooks, enableChdir)
		output.Hint("%s", "You can use git-hop normally:\n"+initNextSteps)
		return
	}
	out := output.ReportOut()
	hubPath, action, registered := path, initActionAlreadyInitialized, false
	if root, ok := resolveBackfillRoot(fs, g, path, structure); ok {
		hubPath = root
		if created, err := backfillHubConfigIfMissing(fs, g, hubPath); err != nil {
			output.Warn("failed to back-fill hop.json at %s: %v", hubPath, err)
		} else if created {
			fmt.Fprintf(out, "Created missing hop.json at %s.\n", hubPath)
			action = initActionAdopted
			registered = registerAdoptedHub(fs, hubPath)
			restoreAdoptedFetchRefspec(g, hubPath)
		}
	}

	printAlreadyInitialized(path, structure)

	if !noHooks {
		if err := installInitHooks(fs, path, "", true); err != nil {
			output.Warn("failed to ensure hooks directory: %v", err)
		} else {
			fmt.Fprintf(out, "\nHooks directory: %s/.git-hop/hooks/\n", path)
		}
	}

	// Mirror any committed .git-hop/hooks/ into the user's hopspace.
	mirrorInitHooks(fs, g, path, path, initHooksMode, initHooksOverwrite, noHooks)

	if enableChdir {
		if err := maybeInstallShellIntegration(fs, true); err != nil {
			output.Warn("failed to install shell integration: %v", err)
		}
	}

	if output.IsStructured() {
		res := initHubResult(fs, g, hubPath, action)
		res.Registered = registered
		emitResult(initRunCmd, res)
		return
	}

	output.Hint("%s", "You can use git-hop normally:\n"+initNextSteps)
}

// printAlreadyInitialized is the summary init prints for a repository
// that already has the worktree structure.
// It is dropped when a structured result stands in for it.
func printAlreadyInitialized(path string, structure config.StructureType) {
	out := output.ReportOut()
	fmt.Fprintln(out, "Repository already initialized with git-hop worktree structure.")
	fmt.Fprintf(out, "Structure: %s\n", structure)
	fmt.Fprintf(out, "Path:      %s\n", path)
}

// installInitHooks installs the .git-hop/hooks directory after init.
// For bare repos it installs in the main worktree; otherwise in the repo root.
func installInitHooks(fs afero.Fs, repoPath, mainWorktreePath string, isRegularRepo bool) error {
	installPath := repoPath
	if mainWorktreePath != "" && !isRegularRepo {
		installPath = mainWorktreePath
	}
	return hooks.NewRunner(fs).InstallHooks(installPath)
}

// mirrorInitHooks scans <worktreePath>/.git-hop/hooks/ for committed hook
// scripts and mirrors them into the user's hopspace at
// <data home>/<hop.dataLayout>/hooks/ (hop.HopspaceHooksDir). Mode resolution:
// CLI flag → GIT_HOP_HOOKS env → hop.hooks.installMode git config → "prompt".
// If --no-hooks was passed and --hooks was not, this is a no-op.
func mirrorInitHooks(fs afero.Fs, g git.GitInterface, worktreePath, repoPath string, flagMode string, overwrite, noHooks bool) {
	mopts, ok := initMirrorOpts(g, worktreePath, repoPath, flagMode, overwrite, noHooks)
	if !ok {
		return
	}
	res, err := hooks.MirrorCommittedHooks(fs, mopts)
	if err != nil {
		output.Warn("failed to mirror committed hooks: %v", err)
		return
	}
	if res.Installed > 0 || res.Warned > 0 || res.Skipped > 0 || res.AlreadyPresent > 0 {
		output.Note("hooks: installed=%d skipped=%d already-present=%d warned=%d",
			res.Installed, res.Skipped, res.AlreadyPresent, res.Warned)
	}
}

// initMirrorOpts resolves the hook mirror mirrorInitHooks runs; ok is
// false, with a warning, when the repository has no org/repo to mirror
// into.
func initMirrorOpts(g git.GitInterface, worktreePath, repoPath string, flagMode string, overwrite, noHooks bool) (hooks.MirrorOpts, bool) {
	// --hooks wins over --no-hooks; otherwise --no-hooks → mode=none.
	mode := flagMode
	if mode == "" && noHooks {
		mode = hooks.ModeNone
	}

	envMode := os.Getenv("GIT_HOP_HOOKS")
	var configured string
	if gc := config.NewGitConfig(); gc != nil {
		configured = gc.GetStringOrDefault(config.KeyHooksInstallMode)
	}
	resolved := hooks.ResolveMode(mode, envMode, configured)

	repoID := initRepoID(g, repoPath)
	if repoID == "" {
		output.Warn("could not determine org/repo for hook mirror; skipping")
		return hooks.MirrorOpts{}, false
	}

	mopts := hooks.MirrorOpts{
		WorktreePath: worktreePath,
		RepoID:       repoID,
		RepoURI:      initRemoteURL(g, repoPath),
		Mode:         resolved,
		Overwrite:    overwrite,
	}
	if resolved == hooks.ModePrompt && isStdinTTYInit() {
		mopts.Stdin = os.Stdin
	}
	return mopts, true
}

func isStdinTTYInit() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// installInitHooksConditional installs hooks unless noHooks is true.
func installInitHooksConditional(fs afero.Fs, repoPath, mainWorktreePath string, isRegularRepo, noHooks bool) {
	if noHooks {
		return
	}
	installInitHooks(fs, repoPath, mainWorktreePath, isRegularRepo) //nolint:errcheck
}

// maybeInstallShellIntegration installs shell integration when enabled is true.
func maybeInstallShellIntegration(fs afero.Fs, enabled bool) error {
	if !enabled {
		return nil
	}
	result, err := shell.InstallIntegration(fs)
	if err != nil {
		return err
	}
	output.Success("Shell integration installed to: %s", result.RcPath)
	output.Info("Restart your shell or run: source %s", result.RcPath)
	return nil
}

// The conversion strategies offered by the init menu.
const (
	choiceBareWorktree    = "1" // bare repo + worktrees (recommended)
	choiceRegularWorktree = "2" // regular repo + worktrees
	choiceRegisterAsIs    = "3" // register current structure unchanged
	choiceQuit            = "q"
)

// initChoices is the set of answers the conversion menu accepts.
var initChoices = []string{choiceBareWorktree, choiceRegularWorktree, choiceRegisterAsIs, choiceQuit}

// resolveInitChoice decides which conversion to run.
//
// With --no-prompt it answers from flags alone and never touches stdin,
// so a scripted or agent-driven init cannot block: --regular selects the
// regular-repo conversion, otherwise the recommended bare conversion.
// Without it the choice comes from the interactive menu.
//
// The returned error is always ErrPromptUnanswerable, meaning the prompt
// could not be put to anyone — an empty stdin, an output mode with no
// prompt channel, or input that never yields a valid choice. It is
// deliberately distinct from the user answering "q", which is a decision
// and stays a clean exit.
func resolveInitChoice(noPrompt, regular bool) (string, error) {
	if noPrompt {
		if regular {
			return choiceRegularWorktree, nil
		}
		return choiceBareWorktree, nil
	}
	return promptInitChoice()
}

// promptInitChoice puts the conversion menu to the user via the shared
// prompt helper, inheriting its bounded-retry and loud-failure contract.
// A piped answer is still a real answer, so `printf '1\n' | git hop init`
// keeps working; the trigger for failing is an unanswerable prompt, not
// a non-TTY stdin.
func promptInitChoice() (string, error) {
	return output.ChoiceAnswer("Choose [1/2/3/q]: ", initChoices)
}

// initHintedFlags lists the init flags named by hints this command
// prints. Tests assert each one is actually declared, so a hint can
// never send a user to a flag that does not exist.
func initHintedFlags() []string {
	return []string{"no-prompt", "regular", "force", "dry-run", "restore"}
}

func init() {
	declareOutputSchema(initCmd, &initResult{})
	initCmd.Flags().BoolVar(&forceFlag, "force", false, "Convert even with uncommitted changes (DANGEROUS; a backup is still taken); with --restore, move what occupies the original location aside (never deleted)")
	initCmd.Flags().BoolVarP(&dryRunFlag, "dry-run", "n", false, "Show conversion steps without executing")
	initCmd.Flags().BoolVar(&keepBackupFlag, "keep-backup", false, "Preserve backup after successful conversion (default: hop.backup.keepBackup)")
	initCmd.Flags().BoolVar(&regularFlag, "regular", false, "Convert to a regular repo + worktrees instead of bare (with --no-prompt)")
	initCmd.Flags().BoolVar(&noPromptFlag, "no-prompt", false, "Skip the interactive menu and convert non-interactively (bare unless --regular)")
	initCmd.Flags().StringVar(&restorePath, "restore", "", "Restore a conversion backup to the location it was taken from (manual rollback)")
	initCmd.Flags().BoolVar(&noHooksFlag, "no-hooks", false, "Skip hooks: no .git-hop/hooks/ directory, no lifecycle hook runs")
	initCmd.Flags().BoolVar(&enableChdirFlag, "enable-chdir", false, "Install shell integration for automatic directory switching after hop commands")
	initCmd.Flags().StringVar(&initHooksMode, "hooks", "", "mirror committed .git-hop/hooks/ into hopspace: symlink|copy|prompt|none (overrides --no-hooks)")
	initCmd.Flags().BoolVar(&initHooksOverwrite, "hooks-overwrite", false, "overwrite an existing hopspace hook with different content (symlink/copy modes)")
}
