package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// pruneRepairBackups removes repair backup directories older than the
// configured retention and retires the legacy <hub>/.hop/ footprint.
// Returns the count of backups removed (or that would be removed when
// dryRun is true); legacy tidying is reported but not counted.
//
// Retention is read from `git config --get hop.repair.backupRetention`
// from any in-scope hub; falls back to 30 days when unconfigured. The
// value uses Go duration syntax (e.g. "720h" for 30 days, "168h" for 7).
func pruneRepairBackups(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool) int {
	retention := repairBackupRetention(g, st)
	cutoff := time.Now().Add(-retention)
	pruned := 0
	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for _, hub := range repo.Hubs {
			pruned += pruneHubRepairBackups(fs, hub.Path, repoID, cutoff, dryRun)
			retireLegacyRepairDir(fs, hub.Path, dryRun)
		}
	}
	return pruned
}

// pruneHubRepairBackups applies retention to every backup root of one
// hub: the state-dir root new snapshots go to and the legacy hub-local
// root earlier releases wrote.
func pruneHubRepairBackups(fs afero.Fs, hubPath, repoID string, cutoff time.Time, dryRun bool) int {
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	pruned := 0
	for _, root := range hop.RepairBackupRoots(hubPath) {
		entries, err := afero.ReadDir(fs, root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "repair-") {
				continue
			}
			if entry.ModTime().After(cutoff) {
				continue
			}
			path := filepath.Join(root, entry.Name())
			output.Info("%s repair backup: %s (%s)", prefix, repoID, path)
			if !dryRun {
				_ = fs.RemoveAll(path)
			}
			pruned++
		}
	}
	return pruned
}

// retireLegacyRepairDir runs hop.CleanupLegacyRepairDir for one hub and
// reports each step as a hint on stderr, git style.
func retireLegacyRepairDir(fs afero.Fs, hubPath string, dryRun bool) {
	steps, err := hop.CleanupLegacyRepairDir(fs, hubPath, dryRun)
	for _, s := range steps {
		switch {
		case s.Kind == "moved" && dryRun:
			fmt.Fprintf(os.Stderr, "hint: [dry-run] would move legacy repair backup %s -> %s\n", s.Path, s.Dest)
		case s.Kind == "moved":
			fmt.Fprintf(os.Stderr, "hint: moved legacy repair backup %s -> %s\n", s.Path, s.Dest)
		case dryRun:
			fmt.Fprintf(os.Stderr, "hint: [dry-run] would remove %s\n", s.Path)
		default:
			fmt.Fprintf(os.Stderr, "hint: removed %s\n", s.Path)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: legacy repair cleanup in %s: %v\n", hubPath, err)
	}
}
