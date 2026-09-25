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
// fixMissingWorktrees owns every state worktree entry whose directory is
// gone: it removes, relocates or keeps each one. What it keeps (the user
// answered "Keep as-is", or git has the worktree locked) is kept for the
// rest of the run, so no pass after it prunes state worktree entries,
// and the hop.json pass skips the kept worktrees' rows. So are the
// worktrees in hubKept, the ones the hub check could not recreate,
// unless the user deletes or relocates their entry at the prompt.
//
// Each repair is recorded in r as it lands (or would); the returned count
// is what the caller adds to r.fixed.
func fixStateIssues(fs afero.Fs, g git.GitInterface, st *state.State, hubPath string, hubKept keptWorktrees, opts doctorOpts, r *doctorReport) int {
	dryRun := !opts.mutating()
	// The repairs are recorded on a scratch report and folded into r
	// below, once their outcome is known: a state save that fails turns
	// every state repair into a failure.
	var fixes doctorReport

	output.Info("\nFixing missing worktrees...")
	missingFixed, kept := fixMissingWorktrees(fs, g, st, hubKept, opts, &fixes)

	output.Info("\nPruning orphaned hubs from state...")
	hubsPruned := pruneOrphanedHubs(fs, st, dryRun)
	for _, p := range hubsPruned {
		recordStatePrune(&fixes, opts, p)
	}

	fixed := missingFixed
	if missingFixed > 0 || len(hubsPruned) > 0 {
		switch {
		case dryRun:
			output.Info("[dry-run] Would prune %d hub(s) from state", len(hubsPruned))
			fixed += len(hubsPruned)
		default:
			if err := state.SaveState(fs, st); err != nil {
				output.Error("Failed to save state: %v", err)
				for i := range fixes.records {
					fixes.records[i].Kind = doctorKindFailed
					fixes.records[i].Message += ": save state: " + err.Error()
				}
			} else {
				output.Info("Pruned %d hub(s) from state", len(hubsPruned))
				fixed += len(hubsPruned)
			}
		}
	}

	fixed += pruneMissingHubRows(fs, g, hubPath, kept, opts, &fixes)

	records, dropped := dedupeRepairs(fixes.records)
	r.records = append(r.records, records...)
	return fixed - dropped
}

// dedupeRepairs drops every fixed or would-fix record whose check and
// subject an earlier one in records already repairs, and returns the
// rest and how many it dropped. A state entry or hop.json row is one
// thing to repair: however many of the state repair's passes reach it,
// the report lists it, and counts it, once.
//
// Scoped to the state repair's records on purpose. Elsewhere one subject
// can take two genuine repairs, e.g. the config check unsetting the same
// key from --global and from the hub's own config.
func dedupeRepairs(records []doctorRecord) ([]doctorRecord, int) {
	type key struct{ check, subject string }
	seen := map[key]bool{}
	out := make([]doctorRecord, 0, len(records))
	for _, rec := range records {
		if rec.Kind == doctorKindFixed || rec.Kind == doctorKindWouldFix {
			k := key{rec.Check, rec.Subject}
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		out = append(out, rec)
	}
	return out, len(records) - len(out)
}

// pruneMissingHubRows drops the current hub's hop.json rows whose
// worktree directory is gone, recording each in r, and returns how many
// it dropped (or, under --dry-run, would drop). Outside a hub (hubPath
// empty) there is no hop.json to rewrite.
//
// A row is kept when its worktree is in kept (the state repair kept it)
// or git has the worktree locked: `git worktree prune` leaves a locked
// worktree whose directory is gone alone, since the directory may only
// be unavailable (a drive that is not mounted), and so do prune and
// doctor. The lock is prune's own check (pruneHubBranchesKeeping); the
// hub check has already warned about the locked rows it skips.
func pruneMissingHubRows(fs afero.Fs, g git.GitInterface, hubPath string, kept keptWorktrees, opts doctorOpts, r *doctorReport) int {
	scoped := stateScopedToHub(hubPath)
	if scoped == nil {
		return 0
	}
	dryRun := !opts.mutating()
	var rows []pruneRecord
	for _, rec := range pruneHubBranchesKeeping(fs, g, scoped, dryRun, kept.has) {
		if rec.Action != pruneActionSkipped {
			rows = append(rows, rec)
		}
	}
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
// the check and subject that reported the problem: the path for a state
// worktree (stateWorktreeSubject) and for a state hub (the state check),
// the branch for a hop.json row (the hub check, which reports the row's
// missing worktree directory).
func recordStatePrune(r *doctorReport, opts doctorOpts, p pruneRecord) {
	switch p.Kind {
	case pruneKindWorktree:
		r.repaired(opts, doctorCheckState, stateWorktreeSubject(p.Repository, p.Branch, p.Path), "prune worktree entry from state")
	case pruneKindHub:
		r.repaired(opts, doctorCheckState, p.Path, "prune hub entry from state")
	case pruneKindHopJSONEntry:
		r.repaired(opts, doctorCheckHub, p.Branch, "prune hop.json entry")
	}
}

// keptWorktrees is the set of missing worktrees doctor leaves in place for
// the whole run, keyed by path. Paths are compared resolved (resolvedPath),
// since state, hop.json and git may each spell the same one differently.
// The zero value (nil) keeps nothing.
type keptWorktrees map[string]struct{}

func (k keptWorktrees) add(path string) { k[resolvedPath(path)] = struct{}{} }

func (k keptWorktrees) remove(path string) { delete(k, resolvedPath(path)) }

func (k keptWorktrees) has(path string) bool {
	_, ok := k[resolvedPath(path)]
	return ok
}

// fixMissingWorktrees handles worktrees whose paths no longer exist on disk.
// For each missing worktree it checks whether the branch was merged into
// the default branch (mergedIntoDefault, the hub check's rule: never the
// default branch itself); if so it removes the state entry automatically. Otherwise it asks the user to either
// provide a new location, delete the entry, or keep it as-is.
// A worktree git has locked is kept without asking: git will not prune
// it, since its directory may only be unavailable (a drive that is not
// mounted).
//
// Returns the number of entries resolved (relocated or deleted), and the
// worktrees kept: locked, kept at the prompt, or left alone because the
// prompt got no usable answer, plus those in hubKept whose entry the
// user did not delete or relocate. The passes after this one must leave
// the kept worktrees alone.
//
// Under dryRun the entry is reported but st is left untouched, and the
// user is never prompted: a preview must not ask for decisions it will
// then discard. An entry that would be put to the user counts as kept,
// since the preview cannot know the answer.
//
// Each resolved entry is recorded in r.
func fixMissingWorktrees(fs afero.Fs, g git.GitInterface, st *state.State, hubKept keptWorktrees, opts doctorOpts, r *doctorReport) (int, keptWorktrees) {
	dryRun := !opts.mutating()
	resolved := 0
	kept := keptWorktrees{}
	for path := range hubKept {
		kept[path] = struct{}{}
	}

	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		// The keys are listed up front: a relocated entry is re-keyed by
		// its new path, and must not be visited again.
		for _, key := range repo.SortedWorktreeKeys() {
			wt := repo.Worktrees[key]
			branch := wt.Branch
			if hop.WorktreeDirPresent(fs, wt.Path) {
				continue
			}
			subject := stateWorktreeSubject(repoID, branch, wt.Path)

			output.Info("\nMissing worktree: %s:%s (was at %s)", repoID, branch, wt.Path)

			if _, locked := stateWorktreeLock(fs, g, repoID, repo.URI, wt); locked {
				output.Info("  Worktree is locked in git; keeping entry.")
				kept.add(wt.Path)
				continue
			}
			// Try to determine a git dir to run branch-merged check.
			// Prefer the hub path recorded in state; fall back to hopspace.
			gitDir := findGitDirForRepo(fs, repoID, repo.URI, wt.HubPath)
			merged := gitDir != "" && mergedIntoDefault(g, gitDir, branch, repo.DefaultBranch)

			if dryRun {
				if merged {
					output.Info("  [dry-run] Would remove entry: branch '%s' is merged into '%s'",
						branch, repo.DefaultBranch)
					r.repaired(opts, doctorCheckState, subject, "remove entry: branch is merged into %s", repo.DefaultBranch)
					resolved++
				} else {
					output.Info("  [dry-run] Would prompt to relocate, delete, or keep entry for '%s'", branch)
					kept.add(wt.Path)
				}
				continue
			}

			if merged {
				output.Info("  Branch '%s' is merged into '%s'; auto-removing entry.", branch, repo.DefaultBranch)
				delete(repo.Worktrees, key)
				r.repaired(opts, doctorCheckState, subject, "remove entry: branch is merged into %s", repo.DefaultBranch)
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
					kept.add(wt.Path)
					continue
				}
				if exists, _ := afero.DirExists(fs, newPath); !exists {
					output.Error("  Path does not exist: %s; skipping.", newPath)
					kept.add(wt.Path)
					continue
				}
				kept.remove(wt.Path)
				delete(repo.Worktrees, key)
				wt.Path = newPath
				_ = st.PutWorktree(repoID, wt)
				output.Info("  Updated path to %s", newPath)
				r.repaired(opts, doctorCheckState, subject, "relocate entry to %s", newPath)
				resolved++

			case 1: // delete
				kept.remove(wt.Path)
				delete(repo.Worktrees, key)
				output.Info("  Deleted entry for '%s'", branch)
				r.repaired(opts, doctorCheckState, subject, "delete entry")
				resolved++

			default: // skip / invalid
				output.Info("  Kept as-is.")
				kept.add(wt.Path)
			}
		}
	}

	return resolved, kept
}

// findGitDirForRepo returns a usable git directory for running git commands
// against the given repository. It tries the hub path first, then falls back
// to the data-home hopspace of the repoID's org/repo (the host, when the data
// layout names one, comes from uri).
func findGitDirForRepo(fs afero.Fs, repoID, uri, hubPath string) string {
	if hubPath != "" {
		if exists, _ := afero.DirExists(fs, hubPath); exists {
			return hubPath
		}
	}

	if ref, ok := hop.RepoRefFromID(repoID, uri); ok {
		hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(), ref)
		if exists, _ := afero.DirExists(fs, hopspacePath); exists {
			return hopspacePath
		}
	}

	return ""
}

// isBranchMerged reports whether `git branch --merged base`, run in dir,
// lists branch. Callers go through mergedIntoDefault.
func isBranchMerged(g git.GitInterface, dir, branch, base string) bool {
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

// stateWorktreeSubject names a state worktree entry in doctor's records:
// its path, which tells two hubs' worktrees of one branch apart (doctor
// pairs an issue with its repair by subject). An entry without a path,
// kept from a file an earlier release wrote, is named repository:branch.
func stateWorktreeSubject(repoID, branch, path string) string {
	if path != "" {
		return path
	}
	return repoID + ":" + branch
}
