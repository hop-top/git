package cmd

import (
	"fmt"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// Where a compare target came from, as named in remove's refusals, human
// output, dry-run and structured records.
const (
	compareSourceOverride      = "override"
	compareSourceBase          = "base"
	compareSourceCompareBranch = "compareBranch"
	compareSourceDefault       = "default"
)

// compareTarget is the branch a worktree is judged against and why that
// branch was picked.
type compareTarget struct {
	// Branch is the branch compared against; empty when the hub has none.
	Branch string
	// Source is one of the compareSource* values.
	Source string
	// Note says why the first choice was passed over: the recorded base
	// no longer exists locally. Empty when nothing was skipped.
	Note string
}

// String renders the target for messages: 'next' (base).
func (t compareTarget) String() string {
	return fmt.Sprintf("'%s' (%s)", t.Branch, t.Source)
}

// resolveCompareSource is the one precedence rule for a worktree's
// comparison branch, with the rung that decided it:
//  1. HubBranch.Base (base)
//  2. HubSettings.CompareBranch (compareBranch)
//  3. RepoConfig.DefaultBranch (default)
//
// status, list, add, remove and doctor all resolve through it.
func resolveCompareSource(cfg *config.HubConfig, b config.HubBranch) (string, string) {
	if b.Base != nil && *b.Base != "" {
		return *b.Base, compareSourceBase
	}
	if cfg.Settings.CompareBranch != nil && *cfg.Settings.CompareBranch != "" {
		return *cfg.Settings.CompareBranch, compareSourceCompareBranch
	}
	return cfg.Repo.DefaultBranch, compareSourceDefault
}

// resolveMergeTarget picks the branch remove and doctor test a worktree's
// merge against: override when given, else resolveCompareSource's choice.
// A recorded base or compare branch that no longer exists locally (it was
// itself merged and removed) is passed over for the next rung, and Note
// says so. dir is any directory of the repository. The default branch is
// the last rung and is used as it stands, except that one with no local
// branch but a remote-tracking ref on origin is named by that ref
// (origin/next).
func resolveMergeTarget(g git.GitInterface, dir string, cfg *config.HubConfig, b config.HubBranch, override string) compareTarget {
	if override != "" {
		return compareTarget{Branch: override, Source: compareSourceOverride}
	}
	name, source := resolveCompareSource(cfg, b)
	t := compareTarget{Branch: name, Source: source}
	if source == compareSourceBase && !localBranchExists(g, dir, name) {
		t.Note = fmt.Sprintf("recorded base '%s' no longer exists", name)
		name, source = resolveCompareSource(cfg, config.HubBranch{})
		t = compareTarget{Branch: name, Source: source, Note: t.Note}
	}
	if source == compareSourceCompareBranch && !localBranchExists(g, dir, name) {
		if t.Note != "" {
			t.Note += "; "
		}
		t.Note += fmt.Sprintf("compareBranch '%s' does not exist", name)
		t.Branch, t.Source = cfg.Repo.DefaultBranch, compareSourceDefault
	}
	if t.Source == compareSourceDefault {
		t.Branch = remoteOnlyRef(g, dir, t.Branch)
	}
	return t
}

// remoteOnlyRef names a branch with no local branch but a remote-tracking
// ref on origin by that ref (next -> origin/next). A branch kept only as a
// remote-tracking ref (no local branch, no worktree) is still where merged
// work lands; left bare, the name resolves to nothing and every branch
// reads as unmerged or unknown. Any other name is returned as it stands.
func remoteOnlyRef(g git.GitInterface, dir, name string) string {
	if name == "" || localBranchExists(g, dir, name) {
		return name
	}
	if remote := "origin/" + name; refExists(g, dir, "refs/remotes/"+remote) {
		return remote
	}
	return name
}

// doctorQuoted is doctorLabel with the branch quoted, as doctor's
// per-entry lines have always shown it: 'main', or 'next' (base).
func (t compareTarget) doctorQuoted() string {
	if t.Source == compareSourceDefault {
		return "'" + t.Branch + "'"
	}
	return fmt.Sprintf("'%s' (%s)", t.Branch, t.Source)
}

// doctorLabel renders the target for doctor's messages: the bare branch
// name for the default branch, as doctor always has, else the branch and
// where it came from (next (base)).
func (t compareTarget) doctorLabel() string {
	if t.Source == compareSourceDefault {
		return t.Branch
	}
	return fmt.Sprintf("%s (%s)", t.Branch, t.Source)
}

// resolveMergeTargetFor is resolveMergeTarget for a known branch, which
// lets the default rung be judged against either of its refs. The local
// default branch can lag origin's (its worktree never pulled after the PR
// merged), and a branch merged into origin/<default> is merged: its work
// lives on a branch that outlasts the worktree. So when branch is not
// merged into the local default but is into origin/<default>, the target
// is origin/<default>, which names the ref that satisfied the check. In
// every other case the target is resolveMergeTarget's. It reads refs as
// they are and never fetches.
func resolveMergeTargetFor(g git.GitInterface, dir string, cfg *config.HubConfig, b config.HubBranch, override, branch string) compareTarget {
	t := resolveMergeTarget(g, dir, cfg, b, override)
	if t.Source != compareSourceDefault || t.Branch == "" || branch == "" || branch == t.Branch {
		return t
	}
	origin := originOfLocal(g, dir, t.Branch)
	if origin == "" {
		return t
	}
	if !branchMergedInto(g, dir, branch, t.Branch) && branchMergedInto(g, dir, branch, origin) {
		t.Branch = origin
	}
	return t
}

// originOfLocal names the remote-tracking ref on origin of a branch that
// also exists locally (next -> origin/next), or "" when either is missing.
// It is the other place a local branch's work can live, and the one that
// runs ahead of it when the local branch was never pulled.
func originOfLocal(g git.GitInterface, dir, name string) string {
	if name == "" || !localBranchExists(g, dir, name) {
		return ""
	}
	if remote := "origin/" + name; refExists(g, dir, "refs/remotes/"+remote) {
		return remote
	}
	return ""
}
