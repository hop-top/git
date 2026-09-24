package cmd

import (
	"strings"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// pruneConversionBackups applies conversion-backup retention to every
// repository in scope and returns the backups removed (or that would be
// removed when dryRun is true).
//
// Retention is per repository: hop.backup.maxBackups counts that
// repository's own backups, and both it and hop.backup.cleanupAgeDays
// are read through the repository's hub (local config first, then
// global), so one repository can keep more than another. Backups are
// looked for under hop.backup.path and under the default cache root, so
// changing the path does not strand the backups taken before.
func pruneConversionBackups(fs afero.Fs, st *state.State, dryRun bool) []pruneRecord {
	now := time.Now()
	claimed := map[string]bool{}
	var pruned []pruneRecord
	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		owner := conversionBackupOwner(repoID, repo)
		gc := repoGitConfig(fs, repo)
		roots := hop.ConversionBackupRoots("")
		if root, err := conversionBackupRoot(gc); err != nil {
			output.Warn("%s: %v; pruning conversion backups in the default location only", repoID, err)
		} else {
			roots = hop.ConversionBackupRoots(root)
		}
		pruned = append(pruned, pruneRepoConversionBackups(fs, repoID, owner, roots, conversionBackupRetention(gc), now, dryRun, claimed)...)
	}
	return pruned
}

// pruneRepoConversionBackups removes one repository's expired conversion
// backups. Backups of failed conversions are reported, never removed;
// backups still being written (no metadata yet) are skipped silently.
// claimed holds backups an earlier repository of the same run already
// considered, so an --all sweep weighs each backup against one
// repository's retention only; it may be nil.
func pruneRepoConversionBackups(fs afero.Fs, repoID string, owner hop.ConversionBackupOwner, roots []string, policy hop.ConversionBackupRetention, now time.Time, dryRun bool, claimed map[string]bool) []pruneRecord {
	listed, err := hop.ListConversionBackups(fs, roots, owner)
	if err != nil {
		output.Warn("%s: cannot list conversion backups: %v", repoID, err)
		return nil
	}
	var backups []hop.ConversionBackup
	for _, b := range listed {
		if claimed[b.Path] {
			continue
		}
		if claimed != nil {
			claimed[b.Path] = true
		}
		backups = append(backups, b)
	}
	for _, b := range backups {
		if b.Failed {
			output.Hint("kept backup of a failed conversion: %s\nremove it by hand once %s is confirmed intact", b.Path, repoID)
		}
	}

	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	var pruned []pruneRecord
	for _, b := range hop.ExpiredConversionBackups(backups, policy, now) {
		output.Info("%s conversion backup: %s (%s)", prefix, repoID, b.Path)
		if !dryRun {
			if err := fs.RemoveAll(b.Path); err != nil {
				output.Warn("failed to remove conversion backup %s: %v", b.Path, err)
				continue
			}
		}
		pruned = append(pruned, newPruneRecord(pruneKindConversionBackup, repoID, "", b.Path, dryRun))
	}
	return pruned
}

// conversionBackupOwner identifies a repository's conversion backups:
// the <org>-<repo> directory init named after it (org and repo from the
// state entry or, failing that, the last two segments of the repository
// ID) and the hubs a backup may have been taken of.
func conversionBackupOwner(repoID string, repo *state.RepositoryState) hop.ConversionBackupOwner {
	var owner hop.ConversionBackupOwner
	if repo != nil {
		owner.Org, owner.Repo = repo.Org, repo.Repo
		for _, hub := range repo.Hubs {
			owner.HubPaths = append(owner.HubPaths, hub.Path)
		}
	}
	if owner.Org == "" || owner.Repo == "" {
		if parts := strings.Split(repoID, "/"); len(parts) >= 2 {
			owner.Org, owner.Repo = parts[len(parts)-2], parts[len(parts)-1]
		}
	}
	return owner
}

// repoGitConfig reads config for a repository through its first hub
// still on disk, so repo-local settings apply; with none left, global
// config only.
func repoGitConfig(fs afero.Fs, repo *state.RepositoryState) *config.GitConfig {
	if repo != nil {
		for _, hub := range repo.Hubs {
			if exists, _ := afero.DirExists(fs, hub.Path); exists {
				return config.NewGitConfigIn(hub.Path)
			}
		}
	}
	return config.NewGlobalGitConfig()
}
