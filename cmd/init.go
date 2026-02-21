package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	forceFlag      bool
	dryRunFlag     bool
	keepBackupFlag bool
	regularFlag    bool
	restorePath    string
)

func init() {
	cli.RootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:     "init",
	Aliases: []string{"setup", "install"},
	Short:   "Initialize git-hop repository structure",
	Long:    `Initialize git-hop repository structure with interactive setup for worktree conversion.`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		g := git.New()

		if restorePath != "" {
			handleRestore(fs, g, restorePath)
			return
		}

		cwd, err := os.Getwd()
		if err != nil {
			output.Error("Failed to get current directory: %v", err)
			os.Exit(1)
		}

		structure := hop.DetectRepoStructure(fs, cwd)
		if structure == config.NotGit {
			output.Error("Not in a git repository")
			os.Exit(1)
		}

		if structure == config.BareWorktreeRoot || structure == config.WorktreeRoot {
			output.Error("Repository already uses worktree structure")
			output.Info("Current structure: %s", structure)
			output.Info("Project root: %s", cwd)
			os.Exit(1)
		}

		if structure != config.StandardRepo {
			output.Error("Repository structure not supported for conversion: %s", structure)
			os.Exit(1)
		}

		showConversionMenu(fs, g, cwd)
	},
}

func showConversionMenu(fs afero.Fs, g git.GitInterface, repoPath string) {
	fmt.Println(`
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Git-Hop Repository Structure
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Current repository: Standard git repository`)

	fmt.Printf("Location: %s\n", repoPath)

	remoteURL, err := g.GetRemoteURL(repoPath)
	if err == nil {
		fmt.Printf("Remote: origin (%s)\n", remoteURL)
	} else {
		fmt.Println("Remote: none")
	}

	branch, err := g.GetCurrentBranch(repoPath)
	if err == nil {
		fmt.Printf("Branch: %s\n", branch)
	}

	fmt.Println("Structure Options:")
	fmt.Println("")
	fmt.Println("  1. Convert to bare repo + worktrees (Recommended)")
	fmt.Println("     Creates bare .git repo + worktree directories")
	fmt.Println("     Preserves all your work and branches")
	fmt.Println("     Backup created automatically")
	fmt.Println("     Follows: Git worktree best practices")
	fmt.Println("")
	fmt.Println("  2. Convert to regular repo + worktrees")
	fmt.Println("     Same worktree structure as option 1")
	fmt.Println("     But allows commits in repo root (not recommended)")
	fmt.Println("     Use if: You need repo root to be working tree")
	fmt.Println("")
	fmt.Println("  3. Register as-is (Limited)")
	fmt.Println("     Uses current repository structure without changes")
	fmt.Println("     Manual worktree management required")
	fmt.Println("     Some git-hop features limited")
	fmt.Println("")
	fmt.Println("  q. Quit")
	fmt.Println("")

	choice := promptChoice("Choose [1/2/3/q]: ", []string{"1", "2", "3", "q"})

	switch choice {
	case "1":
		convertRepo(fs, g, repoPath, true, false)
	case "2":
		convertRepo(fs, g, repoPath, false, false)
	case "3":
		registerAsIs(fs, g, repoPath)
	case "q":
		fmt.Println("Cancelled")
		os.Exit(0)
	}
}

func convertRepo(fs afero.Fs, g git.GitInterface, repoPath string, useBare, isRegular bool) {
	converter := hop.NewConverter(fs, g)
	converter.DryRun = dryRunFlag
	converter.Force = forceFlag
	converter.KeepBackup = keepBackupFlag

	if !dryRunFlag && !forceFlag {
		status, _ := g.RunInDir(repoPath, "git", "status", "--porcelain")
		if status != "" {
			output.Error("Repository has uncommitted changes")
			fmt.Println(`
Bare repository conversion requires clean repository.

Please commit or stash changes before converting:
  git commit -m "WIP: Save work"
  # OR
  git stash push -m "WIP: Save work"

Then run: git hop init

To disable this check:
  git hop config bareRepo false
  # Then run: git hop init --regular

Or register current structure: git hop init --current`)
			os.Exit(1)
		}
	}

	if dryRunFlag {
		fmt.Println("DRY RUN - No changes will be made")
		fmt.Printf("Repository: %s\n", repoPath)

		remoteURL, _ := g.GetRemoteURL(repoPath)
		fmt.Printf("Remote: %s\n", remoteURL)

		branch, _ := g.GetCurrentBranch(repoPath)
		fmt.Printf("Branch: %s\n", branch)

		status, _ := g.RunInDir(repoPath, "git", "status", "--porcelain")
		if status == "" {
			fmt.Println("Status: clean")
		} else {
			fmt.Println("Status: dirty")
		}

		fmt.Println("\nConversion plan:")
		fmt.Println("  1. Create backup in $XDG_CACHE_HOME/git-hop/")
		fmt.Println("  2. Create worktree structure")

		if useBare {
			fmt.Println("     - Convert to bare repository")
		}

		fmt.Println("     - Create main/ worktree for current branch")
		fmt.Println("  3. Create hop.json configuration")
		fmt.Println("  4. Register in global registry")

		fmt.Println("\nTo proceed with conversion, run:")
		fmt.Println("  git hop init")
		return
	}

	output.Info("Converting repository...")

	result, err := converter.ConvertToBareWorktree(repoPath, useBare, true)
	if err != nil {
		output.Error("Conversion failed: %v", err)

		for _, errMsg := range result.Errors {
			fmt.Printf("  - %s\n", errMsg)
		}

		os.Exit(1)
	}

	// Load hub config to get actual worktree path
	hub, err := hop.LoadHub(fs, repoPath)
	var mainWorktreePath string
	var currentBranchName string
	var isRegularRepo bool

	if err == nil {
		// Find current branch worktree
		for name, branch := range hub.Config.Branches {
			currentBranchName = name
			if branch.Path == "." {
				// Regular repo - current branch is in repo root
				mainWorktreePath = repoPath
				isRegularRepo = true
			} else {
				// Bare repo - path is full path to worktree
				mainWorktreePath = filepath.Join(repoPath, branch.Path)
				isRegularRepo = false
			}
			break
		}
	}

	// Update current symlink to point to the main worktree
	if mainWorktreePath != "" && !isRegularRepo {
		if err := hop.UpdateCurrentSymlink(fs, repoPath, mainWorktreePath); err != nil {
			fmt.Printf("Warning: failed to create current symlink: %v\n", err)
		}
	}

	fmt.Println("\nConversion successful!")
	if isRegularRepo {
		fmt.Printf("Project structure:\n")
		fmt.Printf("  %s/\n", repoPath)
		fmt.Printf("    .git/              (repository)\n")
		fmt.Printf("    hop.json\n")
		fmt.Printf("    worktrees/         (future branch worktrees)\n")
		fmt.Printf("    (repo root is %s branch working tree)\n", currentBranchName)
	} else {
		fmt.Printf("Project structure:\n")
		fmt.Printf("  %s/\n", repoPath)
		fmt.Printf("    .git/              (bare repository)\n")
		fmt.Printf("    hop.json\n")
		fmt.Printf("    hops/\n")
		fmt.Printf("      %s/              (worktree for %s branch)\n", currentBranchName, currentBranchName)
		fmt.Printf("    current -> hops/%s  (symlink)\n", currentBranchName)
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	if keepBackupFlag || len(result.Warnings) > 0 {
		fmt.Printf("\nBackup preserved at: %s\n", result.BackupPath)
		fmt.Println("To remove backup manually:")
		fmt.Printf("  rm -rf %s\n", result.BackupPath)
	}

	output.Info("\nYou can now:")
	if !isRegularRepo {
		fmt.Printf("  cd %s   # Work on %s branch\n", mainWorktreePath, currentBranchName)
	}
	fmt.Println("  git hop add <branch>       # Add new branch")
	fmt.Println("  git hop <branch>           # Jump to worktree")
	fmt.Println("  git hop                    # List all worktrees")
}

func registerAsIs(fs afero.Fs, g git.GitInterface, repoPath string) {
	output.Info("Registering repository as-is...")

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

	registry := hop.LoadRegistry(fs)
	repoKey := org + "/" + repo

	if err := registry.AddHop(repoKey, branch, repoPath); err != nil {
		output.Error("Failed to register repository: %v", err)
		os.Exit(1)
	}

	fmt.Println("Repository registered successfully!")
	fmt.Printf("  Repo: %s\n", repoKey)
	fmt.Printf("  Branch: %s\n", branch)
	fmt.Printf("  Path: %s\n", repoPath)

	if remoteURL == "" {
		fmt.Println("\nNote: Repository has no remote configured.")
	}
	fmt.Println("\nNote: Some git-hop features are limited with this structure.")
	fmt.Println("Consider converting to worktree structure for full functionality:")
	fmt.Println("  git hop init --convert")
}

func handleRestore(fs afero.Fs, g git.GitInterface, backupPath string) {
	cwd, err := os.Getwd()
	if err != nil {
		output.Error("Failed to get current directory: %v", err)
		os.Exit(1)
	}

	output.Info("Restoring from backup...")

	converter := hop.NewConverter(fs, g)
	if err := converter.RestoreFromBackup(backupPath, cwd); err != nil {
		output.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	fmt.Println("\nRestore successful!")
	fmt.Printf("Repository restored to: %s\n", cwd)
	fmt.Printf("Original backup: %s\n", backupPath)

	fmt.Println("\nYou can now inspect or delete the backup:")
	fmt.Printf("  rm -rf %s\n", backupPath)
}

func promptChoice(prompt string, validChoices []string) string {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(prompt)
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		for _, valid := range validChoices {
			if choice == valid {
				return choice
			}
		}

		fmt.Println("Invalid choice. Please try again.")
	}
}

func init() {
	initCmd.Flags().BoolVar(&forceFlag, "force", false, "Skip clean repo check and backup requirements (DANGEROUS)")
	initCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show conversion steps without executing")
	initCmd.Flags().BoolVar(&keepBackupFlag, "keep-backup", false, "Preserve backup after successful conversion")
	initCmd.Flags().BoolVar(&regularFlag, "regular", false, "Use regular repo instead of bare")
	initCmd.Flags().StringVar(&restorePath, "restore", "", "Restore repository from backup (manual rollback)")
}

func parseRepoFromURL(uri string) (org, repo string) {
	trimmed := strings.TrimSuffix(uri, ".git")

	if strings.Contains(trimmed, "://") {
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	}

	if strings.HasPrefix(trimmed, "git@") {
		parts := strings.Split(trimmed, ":")
		if len(parts) == 2 {
			path := parts[1]
			pathParts := strings.Split(path, "/")
			if len(pathParts) >= 2 {
				return pathParts[len(pathParts)-2], pathParts[len(pathParts)-1]
			}
		}
	}

	return "", ""
}
