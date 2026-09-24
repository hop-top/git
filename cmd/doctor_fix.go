package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// fixStateIssues is doctor's --fix pass over stale bookkeeping. It returns
// the number of entries that changed — or, under --dry-run, the number
// that would change.
//
// Three surfaces, not one: the global state.json worktree/hub rows, and
// the current hub's hop.json branch rows. `git hop status` renders its hub
// table from hop.json, so a --fix that only cleared state left every
// deleted worktree listed as Missing — the user's view was untouched by
// the fix. The hop.json half reuses prune's helper (repair's
// ActionUpdateHopJSON + Applier, with RepairBackup snapshotting first), so
// a doctor rewrite is undoable via 'git hop repair --undo'.
//
// Scoped deliberately: prune is a global command and visits every hub in
// state, but doctor runs from one hub, so only that hub's hop.json is
// eligible. Outside a hub (hubPath empty) the hop.json pass is skipped.
//
// Under opts.dryRun nothing is written: the state edits are skipped, the
// state.json save is skipped, and pruneOrphanedHubBranches is handed
// dryRun so it neither rewrites hop.json nor takes a backup snapshot — a
// backup is itself a write, and a preview must leave no trace.
//
// Each repair is recorded in r as it lands (or would); the returned count
// is what the caller adds to r.fixed.
func fixStateIssues(fs afero.Fs, g git.GitInterface, st *state.State, hubPath string, opts doctorOpts, r *doctorReport) int {
	dryRun := !opts.mutating()
	// The repairs are recorded on a scratch report and folded into r
	// below, once their outcome is known: a state save that fails turns
	// every state repair into a failure.
	var fixes doctorReport

	output.Info("\nFixing missing worktrees...")
	missingFixed := fixMissingWorktrees(fs, g, st, opts, &fixes)

	output.Info("\nPruning remaining orphaned entries from state...")
	worktreesPruned := pruneOrphanedWorktrees(fs, st, dryRun)
	hubsPruned := pruneOrphanedHubs(fs, st, dryRun)
	for _, p := range append(worktreesPruned, hubsPruned...) {
		recordStatePrune(&fixes, opts, p)
	}

	fixed := missingFixed
	if missingFixed > 0 || len(worktreesPruned) > 0 || len(hubsPruned) > 0 {
		switch {
		case dryRun:
			output.Info("[dry-run] Would prune %d worktree(s) and %d hub(s) from state",
				len(worktreesPruned), len(hubsPruned))
			fixed += len(worktreesPruned) + len(hubsPruned)
		default:
			if err := state.SaveState(fs, st); err != nil {
				output.Error("Failed to save state: %v", err)
				for i := range fixes.records {
					fixes.records[i].Kind = doctorKindFailed
					fixes.records[i].Message += ": save state: " + err.Error()
				}
			} else {
				output.Info("Pruned %d worktree(s) and %d hub(s) from state", len(worktreesPruned), len(hubsPruned))
				fixed += len(worktreesPruned) + len(hubsPruned)
			}
		}
	}

	fixed += pruneMissingHubRows(fs, g, hubPath, opts, &fixes)

	r.records = append(r.records, fixes.records...)
	return fixed
}

// pruneMissingHubRows drops the current hub's hop.json rows whose
// worktree directory is gone, recording each in r, and returns how many
// it dropped (or, under --dry-run, would drop). Outside a hub (hubPath
// empty) there is no hop.json to rewrite.
func pruneMissingHubRows(fs afero.Fs, g git.GitInterface, hubPath string, opts doctorOpts, r *doctorReport) int {
	scoped := stateScopedToHub(hubPath)
	if scoped == nil {
		return 0
	}
	dryRun := !opts.mutating()
	rows := pruneOrphanedHubBranches(fs, g, scoped, dryRun)
	if len(rows) == 0 {
		return 0
	}
	if dryRun {
		output.Info("[dry-run] Would prune %d hop.json entry(ies) from %s", len(rows), hubPath)
	} else {
		output.Info("Pruned %d hop.json entry(ies) from %s", len(rows), hubPath)
	}
	for _, p := range rows {
		recordStatePrune(r, opts, p)
	}
	return len(rows)
}

// recordStatePrune records one entry doctor's state repair prunes, under
// the check and subject that reported the problem: repository:branch
// for a state worktree and the path for a state hub (the state check),
// the branch for a hop.json row (the hub check, which reports the row's
// missing worktree directory).
func recordStatePrune(r *doctorReport, opts doctorOpts, p pruneRecord) {
	switch p.Kind {
	case pruneKindWorktree:
		r.repaired(opts, doctorCheckState, p.Repository+":"+p.Branch, "prune worktree entry from state")
	case pruneKindHub:
		r.repaired(opts, doctorCheckState, p.Path, "prune hub entry from state")
	case pruneKindHopJSONEntry:
		r.repaired(opts, doctorCheckHub, p.Branch, "prune hop.json entry")
	}
}

// fixMissingWorktrees handles worktrees whose paths no longer exist on disk.
// For each missing worktree it checks whether the branch was merged; if so it
// removes the state entry automatically. Otherwise it asks the user to either
// provide a new location, delete the entry, or keep it as-is.
// Returns the number of entries resolved (relocated or deleted).
//
// Under dryRun the entry is reported but st is left untouched, and the
// user is never prompted: a preview must not ask for decisions it will
// then discard.
//
// Each resolved entry is recorded in r.
func fixMissingWorktrees(fs afero.Fs, g git.GitInterface, st *state.State, opts doctorOpts, r *doctorReport) int {
	dryRun := !opts.mutating()
	resolved := 0

	for repoID, repo := range st.Repositories {
		for branch, wt := range repo.Worktrees {
			if exists, _ := afero.DirExists(fs, wt.Path); exists {
				continue
			}

			output.Info("\nMissing worktree: %s:%s (was at %s)", repoID, branch, wt.Path)

			// Try to determine a git dir to run branch-merged check.
			// Prefer the hub path recorded in state; fall back to hopspace.
			gitDir := findGitDirForRepo(fs, repoID, wt.HubPath)
			merged := gitDir != "" && isBranchMerged(g, gitDir, branch, repo.DefaultBranch)

			if dryRun {
				if merged {
					output.Info("  [dry-run] Would remove entry: branch '%s' is merged into '%s'",
						branch, repo.DefaultBranch)
					r.repaired(opts, doctorCheckState, repoID+":"+branch, "remove entry: branch is merged into %s", repo.DefaultBranch)
					resolved++
				} else {
					output.Info("  [dry-run] Would prompt to relocate, delete, or keep entry for '%s'", branch)
				}
				continue
			}

			if merged {
				output.Info("  Branch '%s' is merged into '%s'; auto-removing entry.", branch, repo.DefaultBranch)
				delete(repo.Worktrees, branch)
				r.repaired(opts, doctorCheckState, repoID+":"+branch, "remove entry: branch is merged into %s", repo.DefaultBranch)
				resolved++
				continue
			}

			// Not merged (or unable to check): ask user.
			idx, _ := output.Select(
				fmt.Sprintf("Worktree for '%s' is missing. What would you like to do?", branch),
				[]string{
					"Provide new location",
					"Delete the entry",
					"Keep as-is (skip)",
				},
			)

			switch idx {
			case 0: // new location
				newPath := output.Input("Enter new path for worktree")
				newPath = strings.TrimSpace(newPath)
				if newPath == "" {
					output.Warn("  No path entered; skipping.")
					continue
				}
				if exists, _ := afero.DirExists(fs, newPath); !exists {
					output.Error("  Path does not exist: %s; skipping.", newPath)
					continue
				}
				wt.Path = newPath
				repo.Worktrees[branch] = wt
				output.Info("  Updated path to %s", newPath)
				r.repaired(opts, doctorCheckState, repoID+":"+branch, "relocate entry to %s", newPath)
				resolved++

			case 1: // delete
				delete(repo.Worktrees, branch)
				output.Info("  Deleted entry for '%s'", branch)
				r.repaired(opts, doctorCheckState, repoID+":"+branch, "delete entry")
				resolved++

			default: // skip / invalid
				output.Info("  Kept as-is.")
			}
		}
	}

	return resolved
}

// findGitDirForRepo returns a usable git directory for running git commands
// against the given repository. It tries the hub path first, then falls back
// to the hopspace derived from the repoID.
func findGitDirForRepo(fs afero.Fs, repoID, hubPath string) string {
	if hubPath != "" {
		if exists, _ := afero.DirExists(fs, hubPath); exists {
			return hubPath
		}
	}

	// repoID is typically "github.com/org/repo" — extract org/repo suffix.
	parts := strings.SplitN(repoID, "/", 3)
	if len(parts) == 3 {
		dataHome := hop.GetGitHopDataHome()
		hopspacePath := hop.GetHopspacePath(dataHome, parts[1], parts[2])
		if exists, _ := afero.DirExists(fs, hopspacePath); exists {
			return hopspacePath
		}
	}

	return ""
}

// isBranchMerged reports whether branch has been merged into defaultBranch
// (or into HEAD when defaultBranch is empty). It uses `git branch --merged`.
func isBranchMerged(g git.GitInterface, dir, branch, defaultBranch string) bool {
	base := defaultBranch
	if base == "" {
		base = "HEAD"
	}

	out, err := g.RunInDir(dir, "git", "branch", "--merged", base)
	if err != nil {
		return false
	}

	for _, line := range strings.Split(out, "\n") {
		// Strip leading markers: "* " (current branch), "+ " (worktree), "  " (others)
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimPrefix(trimmed, "+ ")
		name := strings.TrimSpace(trimmed)
		if name == branch {
			return true
		}
	}

	return false
}
