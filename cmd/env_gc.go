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

It also removes the dependency stores earlier releases kept under the
data home, once no worktree of any hub git-hop knows of links into them,
and the cached compose overrides no worktree of any such hub uses.

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

		// Collect the worktrees that are there; one whose path a file
		// occupies would fail the whole collection with ENOTDIR.
		output.Info("Scanning worktrees...")
		worktrees := auditableWorktrees(fs, hub)

		output.Info("  Found %d worktree(s)", len(worktrees))

		// An install an earlier release left in the old layout may be
		// linked from another hub's worktree; gc checks all of them
		// before removing one, and without them keeps it.
		if _, scope, err := depsLinkScope(fs, hubPath); err != nil {
			output.Warn("Cannot list every hub's worktrees, keeping old-layout dependencies: %v", err)
		} else {
			depsManager.SetLinkScope(scope)
		}

		// Run garbage collection
		orphaned, totalSize, err := depsManager.GarbageCollect(worktrees, true)
		if err != nil {
			output.Fatal("Failed to run garbage collection: %v", err)
		}

		// Sizes and last use are read now: once deleted, neither is left
		// to read.
		records := envGCRecords(fs, depsManager.Registry, hopspacePath, orphaned)

		legacy, err := unlinkedLegacyDepsStores(fs, hubPath)
		if err != nil {
			output.Warn("Cannot check old dependency stores, keeping them: %v", err)
		}
		records = append(records, legacyGCRecords(legacy)...)

		overrides, err := orphanedOverrideDirs(fs, hubPath)
		if err != nil {
			output.Warn("Cannot check compose override caches, keeping them: %v", err)
		}
		records = append(records, overrideGCRecords(overrides)...)

		if len(orphaned) == 0 && len(legacy) == 0 && len(overrides) == 0 {
			if output.IsStructured() {
				emitResult(cmd, records)
				return
			}
			output.Info("\nNo orphaned dependencies found. Everything is clean!")
			return
		}

		// Display orphaned dependencies
		if len(orphaned) > 0 {
			output.Info("\nOrphaned dependencies:")
		}
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
			depsPath := filepath.Join(services.DepsStorePath(hopspacePath), depsKey)
			size := getDirSize(fs, depsPath)
			sizeMB := float64(size) / 1024 / 1024

			output.Info("  %s  (last used: %s)  ~%.1fMB", depsKey, lastUsedStr, sizeMB)
		}

		if len(legacy) > 0 {
			output.Info("\nOld dependency stores no worktree links to:")
		}
		for _, s := range legacy {
			output.Info("  %s  ~%.1fMB", s.Path, mb(s.Size))
			totalSize += s.Size
		}

		if len(overrides) > 0 {
			output.Info("\nCompose override caches no worktree uses:")
		}
		for _, d := range overrides {
			output.Info("  %s", d.Path)
			totalSize += d.Size
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
		var deleted []envGCRecord
		totalSize = 0
		if len(orphaned) > 0 {
			output.Info("\nDeleting orphaned dependencies...")
			orphaned, totalSize, err = depsManager.GarbageCollect(worktrees, false)
			if err != nil {
				output.Fatal("Failed to delete orphaned dependencies: %v", err)
			}
			deleted = deletedEnvGCRecords(records, orphaned, hopspacePath)
			output.Info("Deleted %d orphaned dependencies", len(orphaned))
		}
		if len(legacy) > 0 {
			output.Info("\nRemoving old dependency stores...")
			removed := removeLegacyDepsStores(fs, hubPath, legacy)
			for _, r := range removed {
				totalSize += r.Size
			}
			deleted = append(deleted, removed...)
			output.Info("Removed %d old dependency store(s)", len(removed))
		}
		if len(overrides) > 0 {
			removed := removeOverrideDirs(fs, hubPath, overrides)
			for _, r := range removed {
				totalSize += r.Size
			}
			deleted = append(deleted, removed...)
			output.Info("Removed %d compose override cache(s)", len(removed))
		}

		if output.IsStructured() {
			if deleted == nil {
				deleted = []envGCRecord{}
			}
			emitResult(cmd, deleted)
			return
		}

		totalSizeMB = float64(totalSize) / 1024 / 1024
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
		path := filepath.Join(services.DepsStorePath(hopspacePath), key)
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
			r = envGCRecord{Key: key, Path: filepath.Join(services.DepsStorePath(hopspacePath), key)}
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
