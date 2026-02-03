package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	gcForce  bool
	gcDryRun bool
)

var envGcCmd = &cobra.Command{
	Use:     "gc",
	Aliases: []string{"cleanup", "clean"},
	Short:   "Garbage collect orphaned dependencies",
	Long: `Garbage collect orphaned dependencies that are no longer used by any branch.

This command:
1. Scans all worktrees to identify which dependencies are in use
2. Finds dependencies that are no longer referenced by any branch
3. Calculates the total space that can be reclaimed
4. Optionally deletes orphaned dependencies to free up disk space

Use --dry-run to preview what would be deleted without actually deleting.
Use --force to skip the confirmation prompt.`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// Find hub
		hubPath, err := hop.FindHub(fs, cwd)
		if err != nil {
			output.Fatal("Not in a hub. This command must be run from a git-hop managed repository.")
		}

		// Load hub config
		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			output.Fatal("Failed to load hub config: %v", err)
		}

		// Get hopspace path
		dataHome := hop.GetGitHopDataHome()
		hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

		// Load global config
		globalLoader := config.NewGlobalLoader()
		globalConfig, err := globalLoader.Load()
		if err != nil {
			output.Warn("Failed to load global config, using defaults: %v", err)
			globalConfig = globalLoader.GetDefaults()
		}

		// Create deps manager
		depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
		if err != nil {
			output.Fatal("Failed to initialize dependency manager: %v", err)
		}

		// Collect all worktree paths
		output.Info("Scanning worktrees...")
		worktrees := make([]string, 0, len(hub.Config.Branches))
		for _, branch := range hub.Config.Branches {
			worktreePath := filepath.Join(hubPath, branch.Path)
			worktrees = append(worktrees, worktreePath)
		}

		output.Info("  ✓ Found %d worktree(s)", len(worktrees))

		// Run garbage collection
		orphaned, totalSize, err := depsManager.GarbageCollect(worktrees, true)
		if err != nil {
			output.Fatal("Failed to run garbage collection: %v", err)
		}

		if len(orphaned) == 0 {
			output.Info("\n✓ No orphaned dependencies found. Everything is clean!")
			return
		}

		// Display orphaned dependencies
		output.Info("\nOrphaned dependencies:")
		for _, depsKey := range orphaned {
			entry, exists := depsManager.Registry.Entries[depsKey]
			var lastUsedStr string
			if exists {
				duration := time.Since(entry.LastUsed)
				if duration < 24*time.Hour {
					lastUsedStr = "today"
				} else if duration < 48*time.Hour {
					lastUsedStr = "yesterday"
				} else {
					days := int(duration.Hours() / 24)
					lastUsedStr = fmt.Sprintf("%d days ago", days)
				}
			} else {
				lastUsedStr = "unknown"
			}

			// Get size of this specific deps
			depsPath := filepath.Join(hopspacePath, "deps", depsKey)
			size := getDirSize(fs, depsPath)
			sizeMB := float64(size) / 1024 / 1024

			output.Info("  %s  (last used: %s)  ~%.1fMB", depsKey, lastUsedStr, sizeMB)
		}

		totalSizeMB := float64(totalSize) / 1024 / 1024
		output.Info("\nTotal reclaimable: %.1fMB", totalSizeMB)

		if gcDryRun {
			output.Info("\n(Dry run - no changes made)")
			return
		}

		// Confirm deletion unless --force
		if !gcForce {
			output.Info("\nDelete these dependencies? [y/N]: ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				output.Info("Cancelled.")
				return
			}
		}

		// Perform deletion
		output.Info("\nDeleting orphaned dependencies...")
		orphaned, totalSize, err = depsManager.GarbageCollect(worktrees, false)
		if err != nil {
			output.Fatal("Failed to delete orphaned dependencies: %v", err)
		}

		totalSizeMB = float64(totalSize) / 1024 / 1024
		output.Info("✓ Deleted %d orphaned dependencies", len(orphaned))
		output.Info("✓ Reclaimed %.1fMB", totalSizeMB)
	},
}

func init() {
	envCmd.AddCommand(envGcCmd)
	envGcCmd.Flags().BoolVar(&gcForce, "force", false, "Skip confirmation prompt")
	envGcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false, "Show what would be deleted without deleting")
}
