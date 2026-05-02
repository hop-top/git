package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initRepoWithRemote builds a hub repo on disk with origin/main fetched, so
// CreateWorktree's --track path has a real upstream to bind to. Returns the
// hub dir and the parent dir that holds it.
func initRepoWithRemote(t *testing.T) (hub string) {
	t.Helper()

	root := t.TempDir()
	upstream := filepath.Join(root, "upstream.git")
	hub = filepath.Join(root, "hub")

	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), out)
	}

	// Bare upstream
	run("", "init", "--bare", "--initial-branch=main", upstream)

	// Hub clone with one commit on main
	run("", "clone", upstream, hub)
	require.NoError(t, os.WriteFile(filepath.Join(hub, "seed"), []byte("seed\n"), 0o644))
	run(hub, "add", "seed")
	run(hub, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	run(hub, "push", "origin", "main")

	// Refresh remote-tracking ref so origin/main exists locally
	run(hub, "fetch", "origin")

	return hub
}

// TestCreateWorktree_RealGit_NewBranchTracksRemote regression-tests the bug
// where CreateWorktree built an argv that real git rejects with exit 129.
//
// The previous mocked test asserted the literal buggy argv string, which
// passed because MockRunner never invokes git. This test invokes real git
// against an on-disk repo, so any argv-shape regression fails fast.
func TestCreateWorktree_RealGit_NewBranchTracksRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (requires git on PATH)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}

	hub := initRepoWithRemote(t)
	g := &Git{Runner: &RealRunner{}}

	wtPath := filepath.Join(filepath.Dir(hub), "wt-feature")

	// CreateWorktree("hub", "feature", path, base, force=false, track="origin/main")
	// must produce a worktree on a new local branch "feature" tracking origin/main.
	err := g.CreateWorktree(hub, "feature", wtPath, "HEAD", false, "origin/main")
	require.NoError(t, err)

	// Worktree dir must exist
	_, err = os.Stat(wtPath)
	require.NoError(t, err, "worktree dir should exist")

	// Branch in the worktree must be "feature"
	out, err := exec.Command("git", "-C", wtPath, "branch", "--show-current").CombinedOutput()
	require.NoError(t, err, "branch --show-current failed: %s", out)
	assert.Equal(t, "feature", strings.TrimSpace(string(out)))

	// Upstream of "feature" must be "origin/main"
	out, err = exec.Command("git", "-C", wtPath,
		"rev-parse", "--abbrev-ref", "feature@{upstream}").CombinedOutput()
	require.NoError(t, err, "feature has no upstream: %s", out)
	assert.Equal(t, "origin/main", strings.TrimSpace(string(out)))
}

// TestCreateWorktree_RealGit_NewBranchNoTracking covers the same path
// without --track, so the regression test isn't trivially satisfied by a
// branch that happens to track HEAD.
func TestCreateWorktree_RealGit_NewBranchNoTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (requires git on PATH)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}

	hub := initRepoWithRemote(t)
	g := &Git{Runner: &RealRunner{}}

	wtPath := filepath.Join(filepath.Dir(hub), "wt-no-track")
	err := g.CreateWorktree(hub, "untracked", wtPath, "HEAD", false, "")
	require.NoError(t, err)

	out, err := exec.Command("git", "-C", wtPath, "branch", "--show-current").CombinedOutput()
	require.NoError(t, err)
	assert.Equal(t, "untracked", strings.TrimSpace(string(out)))

	// No upstream configured
	cmd := exec.Command("git", "-C", wtPath,
		"rev-parse", "--abbrev-ref", "untracked@{upstream}")
	require.Error(t, cmd.Run(), "untracked branch should not have an upstream")
}
