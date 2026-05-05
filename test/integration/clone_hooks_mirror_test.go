package integration_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/hooks"
)

// TestMirrorCommittedHooks_Symlink_RealFs exercises the symlink install
// path against a real on-disk worktree containing a committed
// .git-hop/hooks/post-worktree-add. This is the integration counterpart to
// the unit table tests in internal/hooks/install_test.go and the regression
// fixture for T-0217: a fresh hopspace must end up with a working pointer
// to the committed hook so post-worktree-add fires for the first worktree.
func TestMirrorCommittedHooks_Symlink_RealFs(t *testing.T) {
	tempDir := t.TempDir()
	dataHome := filepath.Join(tempDir, "data")
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)

	// Initialize a git repo to mirror the layout cmd/init.go and
	// CloneWorktree produce after a clone.
	mustRun(t, tempDir, "git", "init", "-b", "main")
	mustRun(t, tempDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, tempDir, "git", "config", "user.name", "Test User")

	hooksDir := filepath.Join(tempDir, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "post-worktree-add")
	hookBody := "#!/bin/sh\necho post-worktree-add fired\n"
	if err := os.WriteFile(hookPath, []byte(hookBody), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	// Commit it so the test mirrors the "committed hook" precondition.
	mustRun(t, tempDir, "git", "add", ".git-hop")
	mustRun(t, tempDir, "git", "commit", "-m", "add hook")

	res, err := hooks.MirrorCommittedHooks(afero.NewOsFs(), hooks.MirrorOpts{
		WorktreePath: tempDir,
		RepoID:       "github.com/testorg/testrepo",
		Mode:         hooks.ModeSymlink,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("MirrorCommittedHooks: %v", err)
	}
	if res.Installed != 1 {
		t.Fatalf("expected Installed=1, got %+v", res)
	}

	dst := filepath.Join(dataHome, "github.com", "testorg", "testrepo",
		"hooks", "post-worktree-add")

	target, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("readlink %s: %v", dst, err)
	}
	absSrc, _ := filepath.Abs(hookPath)
	if target != absSrc {
		t.Errorf("symlink target = %q; want %q", target, absSrc)
	}

	// Resolved file content must match what was committed (sanity check
	// that the symlink works end-to-end).
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read via symlink: %v", err)
	}
	if string(got) != hookBody {
		t.Errorf("content via symlink = %q; want %q", string(got), hookBody)
	}
}

// TestMirrorCommittedHooks_NonInteractivePromptDegrades verifies that when
// a clone runs in CI (no TTY, no Stdin), --hooks=prompt becomes a no-op
// rather than blocking forever waiting for input.
func TestMirrorCommittedHooks_NonInteractivePromptDegrades(t *testing.T) {
	tempDir := t.TempDir()
	dataHome := filepath.Join(tempDir, "data")
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)

	hooksDir := filepath.Join(tempDir, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "post-worktree-add"),
		[]byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := hooks.MirrorCommittedHooks(afero.NewOsFs(), hooks.MirrorOpts{
		WorktreePath: tempDir,
		RepoID:       "github.com/testorg/testrepo",
		Mode:         hooks.ModePrompt,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 {
		t.Fatalf("non-interactive prompt should not install: %+v", res)
	}
	dst := filepath.Join(dataHome, "github.com", "testorg", "testrepo",
		"hooks", "post-worktree-add")
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("expected dst not to exist; got err=%v", err)
	}
}
