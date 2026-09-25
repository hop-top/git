package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/test/mocks"
)

// git lists a worktree by its resolved path; hop.json may record it
// through a symlink (macOS /var is /private/var). Both name the same
// worktree, or remove would take a live worktree for an unregistered
// directory and refuse to remove it.
func TestIsWorktreeRegistered_ThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "hops", "feat")
	require.NoError(t, os.MkdirAll(wt, 0o755))
	link := filepath.Join(t.TempDir(), "hub")
	require.NoError(t, os.Symlink(dir, link))
	resolved, err := filepath.EvalSymlinks(wt)
	require.NoError(t, err)

	g := mocks.NewMockGit()
	g.WorktreeListOut = porcelainFor(resolved)

	assert.True(t, isWorktreeRegistered(g, link, filepath.Join(link, "hops", "feat")))
	assert.False(t, isWorktreeRegistered(g, link, filepath.Join(link, "hops", "other")))
}
