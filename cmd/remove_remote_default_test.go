package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// remoteOnlyDefaultRepo builds a repository whose default branch, next,
// exists only as refs/remotes/origin/next (no refs/heads/next), as in a
// hub whose default branch was never checked out. feat's commit was
// rebase-merged into next: the same patch under a new SHA, so feat's tip
// is not reachable from it. other is a branch whose work never landed.
// The worktree is on feat.
func remoteOnlyDefaultRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commitFile := func(name string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644))
		run("add", name)
		run("commit", "-q", "-m", "add "+name)
	}

	run("init", "-q", "-b", "main")
	commitFile("base")
	run("checkout", "-q", "-b", "feat")
	commitFile("feat")
	run("checkout", "-q", "-b", "other", "main")
	commitFile("other")

	// origin/next: main plus an unrelated commit, then feat's commit
	// replayed on top (the rebase-merge).
	run("checkout", "-q", "-b", "landing", "main")
	commitFile("unrelated")
	run("cherry-pick", "feat")
	run("update-ref", "refs/remotes/origin/next", "landing")
	run("checkout", "-q", "feat")
	run("branch", "-D", "landing")
	return dir
}

func remoteOnlyDefaultHub(def string) *config.HubConfig {
	return &config.HubConfig{Repo: config.RepoConfig{DefaultBranch: def}}
}

// A default branch with no local branch but an origin tracking ref is
// compared against as origin/<default>: the bare name resolves to nothing.
func TestResolveMergeTarget_DefaultOnlyOnOrigin(t *testing.T) {
	dir := remoteOnlyDefaultRepo(t)
	g := git.New()

	got := resolveMergeTarget(g, dir, remoteOnlyDefaultHub("next"), config.HubBranch{}, "")
	assert.Equal(t, "origin/next", got.Branch)
	assert.Equal(t, compareSourceDefault, got.Source)

	// A local default branch is used as it stands.
	got = resolveMergeTarget(g, dir, remoteOnlyDefaultHub("main"), config.HubBranch{}, "")
	assert.Equal(t, "main", got.Branch)

	// Neither local nor remote: the name stands, the gate fails closed.
	got = resolveMergeTarget(g, dir, remoteOnlyDefaultHub("gone"), config.HubBranch{}, "")
	assert.Equal(t, "gone", got.Branch)
}

// A branch rebase-merged into a default branch that exists only on origin
// is merged: removable with no flags once clean. A branch whose work never
// landed still needs --force.
func TestRemoveSafety_RebaseMergedIntoOriginOnlyDefault(t *testing.T) {
	dir := remoteOnlyDefaultRepo(t)
	g := git.New()
	target := resolveMergeTarget(g, dir, remoteOnlyDefaultHub("next"), config.HubBranch{}, "")

	s := inspectBranchSafety(g, dir, "feat", target.Branch)
	assert.True(t, s.Merged, "feat's patch is on origin/next")
	assert.True(t, s.Clean)
	assert.NoError(t, removeGate(s, false, false), "merged and clean needs no flags")

	s = inspectBranchSafety(g, dir, "other", target.Branch)
	assert.False(t, s.Merged, "other never landed on origin/next")
	assert.Error(t, removeGate(s, false, false))
}

// With no worktree for the default branch, the removal of the worktree
// "current" names must not leave "current" dangling.
func TestUpdateCurrentToDefault_NoDefaultWorktreeDropsDanglingCurrent(t *testing.T) {
	fs := afero.NewOsFs()
	hubPath := t.TempDir()
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "next")
	require.NoError(t, err)

	otherPath := filepath.Join(hubPath, "hops", "main")
	featPath := filepath.Join(hubPath, "hops", "feat")
	require.NoError(t, os.MkdirAll(otherPath, 0o755))
	require.NoError(t, os.MkdirAll(featPath, 0o755))
	require.NoError(t, hub.AddBranch("main", "main", otherPath))
	require.NoError(t, hub.AddBranch("feat", "feat", featPath))
	require.NoError(t, hop.UpdateCurrentSymlink(fs, hubPath, featPath))

	// feat is removed: its directory and its hub entry go.
	require.NoError(t, os.RemoveAll(featPath))
	require.NoError(t, hub.RemoveBranch("feat"))

	require.NoError(t, updateCurrentToDefault(fs, hub, hubPath))

	_, err = os.Lstat(filepath.Join(hubPath, "current"))
	assert.True(t, os.IsNotExist(err), "current must not be left dangling, got err=%v", err)
}

// A "current" that still resolves is not touched when the default branch
// has no worktree: the removed worktree was not what it pointed at.
func TestUpdateCurrentToDefault_NoDefaultWorktreeKeepsLiveCurrent(t *testing.T) {
	fs := afero.NewOsFs()
	hubPath := t.TempDir()
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "next")
	require.NoError(t, err)

	mainPath := filepath.Join(hubPath, "hops", "main")
	require.NoError(t, os.MkdirAll(mainPath, 0o755))
	require.NoError(t, hub.AddBranch("main", "main", mainPath))
	require.NoError(t, hop.UpdateCurrentSymlink(fs, hubPath, mainPath))

	assert.Error(t, updateCurrentToDefault(fs, hub, hubPath))
	target, err := os.Readlink(filepath.Join(hubPath, "current"))
	require.NoError(t, err)
	assert.Equal(t, "hops/main", target)
}
