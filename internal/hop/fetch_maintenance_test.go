package hop

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/git"
	"hop.top/git/internal/git/gittrace"
)

// A fetch git-hop runs right before `git worktree add` must not start
// git's auto-maintenance. That run detaches, and since git 2.54 it
// includes worktree-prune: landing between add creating worktrees/<name>
// and writing its lock, it deletes the fresh entry and the add dies with
// "could not open 'worktrees/main/locked' for writing".
//
// The race itself cannot be timed from here, so these assert on its
// precondition: no maintenance child anywhere in git's own trace.

func TestCloneBareRepo_FetchStartsNoAutoMaintenance(t *testing.T) {
	dir := t.TempDir()
	src := committedRepo(t, filepath.Join(dir, "src"))

	trace := gittrace.Start(t)
	hub := filepath.Join(dir, "hub")
	require.NoError(t, cloneBareRepo(afero.NewOsFs(), git.New(), src, hub, "main"))
	trace.RequireNoAutoMaintenance(t)
}

// init copies the source's remote-tracking refs into the new hub with a
// fetch, then adds the default worktree.
func TestConvertToBareWorktree_FetchStartsNoAutoMaintenance(t *testing.T) {
	dir := t.TempDir()
	upstream := committedRepo(t, filepath.Join(dir, "upstream"))
	repo := filepath.Join(dir, "org", "proj")
	runGit(t, "clone", "-q", upstream, repo)
	runGit(t, "-C", repo, "remote", "set-url", "origin", "https://github.com/org/proj.git")

	trace := gittrace.Start(t)
	conv := NewConverter(afero.NewOsFs(), git.New())
	conv.KeepBackup = false
	_, err := conv.ConvertToBareWorktree(repo, true, true)
	require.NoError(t, err)
	trace.RequireNoAutoMaintenance(t)
}

func committedRepo(t *testing.T, path string) string {
	t.Helper()
	runGit(t, "init", "-q", "-b", "main", path)
	runGit(t, "-C", path, "commit", "-q", "--allow-empty", "-m", "init")
	return path
}
