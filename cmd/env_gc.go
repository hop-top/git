package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

var (
	gcForce    bool
	gcNoPrompt bool
	gcDryRun   bool
)

var envGcCmd = &cobra.Command{
	Use:     "gc",
	Args:    cobra.NoArgs,
	Aliases: []string{"cleanup", "clean"},
	Short:   "Garbage collect orphaned dependencies",
	Long: `Garbage collect orphaned dependencies that are no longer used by any branch.

This command:
1. Scans all worktrees to identify which dependencies are in use
2. Finds dependencies that are no longer referenced by any branch
3. Calculates the total space that can be reclaimed
4. Optionally deletes orphaned dependencies to free up disk space

Use --dry-run to preview what would be deleted without actually deleting.
Use --no-prompt (or --force) to skip the confirmation prompt; without it a
non-interactive run with nothing to read on stdin fails rather than
silently cancelling.`,
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

		hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)

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
		worktrees := hub.WorktreePaths()

		output.Info("  Found %d worktree(s)", len(worktrees))

		// Run garbage collection
		orphaned, totalSize, err := depsManager.GarbageCollect(worktrees, true)
		if err != nil {
			output.Fatal("Failed to run garbage collection: %v", err)
		}

		// Sizes and last use are read now: once deleted, neither is left
		// to read.
		records := envGCRecords(fs, depsManager.Registry, hopspacePath, orphaned)

		if len(orphaned) == 0 {
			if output.IsStructured() {
				emitResult(cmd, records)
				return
			}
			output.Info("\nNo orphaned dependencies found. Everything is clean!")
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
			if output.IsStructured() {
				emitResult(cmd, records)
				return
			}
			output.Info("\n(Dry run - no changes made)")
			return
		}

		if !confirmEnvGC(gcNoPrompt || gcForce) {
			return
		}

		// Perform deletion
		output.Info("\nDeleting orphaned dependencies...")
		orphaned, totalSize, err = depsManager.GarbageCollect(worktrees, false)
		if err != nil {
			output.Fatal("Failed to delete orphaned dependencies: %v", err)
		}

		if output.IsStructured() {
			emitResult(cmd, deletedEnvGCRecords(records, orphaned, hopspacePath))
			return
		}

		totalSizeMB = float64(totalSize) / 1024 / 1024
		output.Info("Deleted %d orphaned dependencies", len(orphaned))
		output.Info("Reclaimed %.1fMB", totalSizeMB)
	},
}

// envGCRecords describes the orphaned dependency directories as the
// would-delete records of a preview, sorted by key. The slice is never
// nil, so an empty result renders as [] rather than null.
func envGCRecords(fs afero.Fs, registry *services.DepsRegistry, hopspacePath string, orphaned []string) []envGCRecord {
	keys := append([]string(nil), orphaned...)
	sort.Strings(keys)

	records := make([]envGCRecord, 0, len(keys))
	for _, key := range keys {
		path := filepath.Join(hopspacePath, "deps", key)
		r := envGCRecord{
			Action: "would-delete",
			Key:    key,
			Size:   getDirSize(fs, path),
			Path:   path,
		}
		if entry, ok := registry.Entries[key]; ok && !entry.LastUsed.IsZero() {
			r.LastUsed = entry.LastUsed.UTC().Format(time.RFC3339)
		}
		records = append(records, r)
	}
	return records
}

// deletedEnvGCRecords marks the deleted keys' records as deleted, sorted
// by key. Size and last use come from the preview taken before deletion;
// a key that only became orphaned after the preview has neither.
func deletedEnvGCRecords(preview []envGCRecord, deleted []string, hopspacePath string) []envGCRecord {
	byKey := make(map[string]envGCRecord, len(preview))
	for _, r := range preview {
		byKey[r.Key] = r
	}
	keys := append([]string(nil), deleted...)
	sort.Strings(keys)

	records := make([]envGCRecord, 0, len(keys))
	for _, key := range keys {
		r, ok := byKey[key]
		if !ok {
			r = envGCRecord{Key: key, Path: filepath.Join(hopspacePath, "deps", key)}
		}
		r.Action = "deleted"
		records = append(records, r)
	}
	return records
}

// confirmEnvGC gates the destructive half of `env gc`.
//
// It routes through the shared confirmation helper rather than reading
// stdin itself, so it inherits the loud-failure contract: an
// unanswerable prompt (nothing readable on stdin) exits non-zero with a
// fatal: message naming --no-prompt instead of printing a cancellation
// that a batch script would read as a successful collection. A piped
// answer is still a real answer, so `echo y | git hop env gc` keeps
// working; the trigger is an unanswerable prompt, not a non-TTY stdin.
func confirmEnvGC(skipPrompt bool) bool {
	if skipPrompt {
		return true
	}
	output.Info("")
	confirmed, err := output.ConfirmAnswer("Delete these dependencies?")
	return resolveConfirmation(confirmed, err)
}

func init() {
	envCmd.AddCommand(envGcCmd)
	envGcCmd.Flags().BoolVar(&gcForce, "force", false, "Skip confirmation prompt")
	envGcCmd.Flags().BoolVar(&gcNoPrompt, "no-prompt", false, "Skip the confirmation prompt (non-interactive callers)")
	envGcCmd.Flags().BoolVarP(&gcDryRun, "dry-run", "n", false, "Show what would be deleted without deleting")
	declareOutputSchema(envGcCmd, &[]envGCRecord{})
}
