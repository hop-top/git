package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout redirects stdout during f and returns what was printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestHandleAlreadyInitialized_BareWorktreeRoot(t *testing.T) {
	// DetectRepoStructure uses os.Stat for HEAD, so we need a real tmpdir.
	repoPath := t.TempDir()
	g := git.New()
	fs := afero.NewOsFs()

	gitDir := filepath.Join(repoPath, ".git")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "worktrees"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644))

	structure := hop.DetectRepoStructure(fs, repoPath)
	assert.Equal(t, config.BareWorktreeRoot, structure)

	out := captureStdout(t, func() {
		handleAlreadyInitialized(fs, g, repoPath, structure)
	})

	assert.Contains(t, out, "already initialized")
}

func TestHandleAlreadyInitialized_WorktreeChild(t *testing.T) {
	// IsWorktree uses os.Stat, so we need a real tmpdir.
	worktreePath := t.TempDir()
	g := git.New()
	fs := afero.NewOsFs()

	// Simulate a WorktreeChild: .git is a file with "gitdir:" content
	require.NoError(t, os.WriteFile(filepath.Join(worktreePath, ".git"),
		[]byte("gitdir: /some/hub/.git/worktrees/feature-x\n"), 0644))

	structure := hop.DetectRepoStructure(fs, worktreePath)
	assert.Equal(t, config.WorktreeChild, structure)

	out := captureStdout(t, func() {
		handleAlreadyInitialized(fs, g, worktreePath, structure)
	})

	assert.Contains(t, out, "already initialized")
}
