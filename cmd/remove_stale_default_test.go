package cmd

import (
	"os/exec"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// staleLocalDefaultRepo is remoteOnlyDefaultRepo plus a local branch next
// left at main: the default branch has a local ref that is behind
// origin/next, as in a hub whose default worktree was never pulled after
// the PR merged. feat's patch is on origin/next only; other never landed;
// localonly is merged into the local next but not into origin/next.
func staleLocalDefaultRepo(t *testing.T) string {
	t.Helper()
	dir := remoteOnlyDefaultRepo(t)
	run := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("branch", "next", "main")
	return dir
}

// feat is rebase-merged into origin/next only: the stale local next does
// not hold it. Merged by content into origin/next, so removable with no
// flags once clean, and the verdict names origin/next.
func TestRemoveSafety_RebaseMergedIntoOriginAheadOfLocalDefault(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	g := git.New()
	hub := remoteOnlyDefaultHub("next")

	target := resolveMergeTargetFor(g, dir, hub, config.HubBranch{}, "", "feat")
	assert.Equal(t, "'origin/next' (default)", target.String())

	s := inspectRemoveSafety(afero.NewOsFs(), g, dir, dir, "feat", target)
	assert.True(t, s.Merged, "feat's patch is on origin/next")
	assert.True(t, s.Clean)
	assert.NoError(t, removeGate(s, false, false), "merged and clean needs no flags")
}

// A branch in neither the local default nor origin's stays unmerged and
// keeps the local default as its target.
func TestRemoveSafety_NotMergedAnywhereWithStaleLocalDefault(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	g := git.New()

	target := resolveMergeTargetFor(g, dir, remoteOnlyDefaultHub("next"), config.HubBranch{}, "", "other")
	assert.Equal(t, "'next' (default)", target.String())

	s := inspectRemoveSafety(afero.NewOsFs(), g, dir, dir, "other", target)
	assert.False(t, s.Merged)
	assert.Error(t, removeGate(s, false, false))
}

// A branch merged only into the local default is merged as before, and
// the target stays the local default.
func TestRemoveSafety_MergedOnlyIntoLocalDefault(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	g := git.New()
	// Local next absorbs other's tip; origin/next never saw it.
	out, err := exec.Command("git", "-C", dir, "branch", "-f", "next", "other").CombinedOutput()
	require.NoError(t, err, string(out))

	target := resolveMergeTargetFor(g, dir, remoteOnlyDefaultHub("next"), config.HubBranch{}, "", "other")
	assert.Equal(t, "'next' (default)", target.String())

	s := inspectRemoveSafety(afero.NewOsFs(), g, dir, dir, "other", target)
	assert.True(t, s.Merged)
	assert.NoError(t, removeGate(s, false, false))
}

// An explicit override is used as given, never swapped for origin's ref.
func TestResolveMergeTargetFor_OverrideKept(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	target := resolveMergeTargetFor(git.New(), dir, remoteOnlyDefaultHub("next"), config.HubBranch{}, "next", "feat")
	assert.Equal(t, "'next' (override)", target.String())
}

// doctor's missing-worktree cleanup judges merged the same way.
func TestMergedMissingBranch_StaleLocalDefault(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	g := git.New()
	hub := &hop.Hub{Path: dir, Config: remoteOnlyDefaultHub("next")}

	target, merged := mergedMissingBranch(g, hub, config.HubBranch{}, "feat")
	assert.True(t, merged)
	assert.Equal(t, "origin/next", target.Branch)

	_, merged = mergedMissingBranch(g, hub, config.HubBranch{}, "other")
	assert.False(t, merged)
}

// status and list read a branch rebase-merged into origin/<default> as
// merged when the local default is behind it; the numbers still count
// against the local default, and a branch that never landed keeps its
// plain label.
func TestStatus_MergedIntoOriginAheadOfLocalDefault(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	g := git.New()

	assert.Equal(t, "merged", getBranchSyncStatus(g, dir, "feat", "next"))
	assert.Equal(t, "1 ahead", getBranchSyncStatus(g, dir, "other", "next"))
}
