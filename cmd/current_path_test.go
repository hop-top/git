package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// realHub builds a hub on the real filesystem: hop.json at the root, a
// worktree at hops/<branch>, and a relative `current` symlink pointing at it
// -- the shape hop.UpdateCurrentSymlink writes.
//
// The real filesystem rather than a memory one because the thing under test
// is symlink resolution, and afero's in-memory backend has no symlinks to
// resolve.
func realHub(t *testing.T, branch string) (hub, worktree string) {
	t.Helper()

	root := t.TempDir()
	hub = filepath.Join(root, "hub")
	worktree = filepath.Join(hub, "hops", filepath.FromSlash(branch))
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hub, "hop.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write hop.json: %v", err)
	}
	rel, err := filepath.Rel(hub, worktree)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if err := os.Symlink(rel, filepath.Join(hub, "current")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return hub, worktree
}

// TestResolveCurrentWorktree_FindsHubAtAnyBranchDepth is the regression the
// whole change exists for.
//
// The wrapper used to compute this in shell, relative to `git rev-parse
// --show-toplevel`, with a fixed number of "..". Branch names nest
// arbitrarily, so no fixed number is right for all of them -- which is why
// the depths are swept here rather than represented by one example.
func TestResolveCurrentWorktree_FindsHubAtAnyBranchDepth(t *testing.T) {
	for _, branch := range []string{"main", "fix/chpwd", "a/b/c"} {
		t.Run(branch, func(t *testing.T) {
			hub, worktree := realHub(t, branch)
			fs := afero.NewOsFs()

			// Every starting point inside the hub must give the same answer:
			// the hub itself, the destination worktree, and a directory
			// nested inside it.
			nested := filepath.Join(worktree, "internal", "deep")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatalf("mkdir nested: %v", err)
			}

			for _, start := range []string{hub, worktree, nested} {
				got, ok := resolveCurrentWorktree(fs, start)
				if !ok {
					t.Fatalf("from %q: no current worktree found", start)
				}
				if got != worktree {
					t.Errorf("from %q: got %q, want %q", start, got, worktree)
				}
			}
		})
	}
}

// TestResolveCurrentWorktree_OutsideAHub is the false-positive guard.
//
// Resolution walks UP for hop.json, so a directory with no hub above it must
// come back empty rather than latching onto something further up the user's
// home directory. An empty answer is what keeps the wrapper from cd'ing
// people out of unrelated repositories.
func TestResolveCurrentWorktree_OutsideAHub(t *testing.T) {
	dir := t.TempDir()
	if _, ok := resolveCurrentWorktree(afero.NewOsFs(), dir); ok {
		t.Errorf("found a current worktree from %q, which is not inside any hub", dir)
	}
}

// TestResolveCurrentWorktree_DanglingSymlink covers a `current` whose target
// was removed outside git-hop.
//
// Reporting the path anyway would hand the wrapper somewhere to cd that does
// not exist, and the shell would surface an error the user did not cause.
// Reporting nothing leaves them where they are, which is the mild direction.
func TestResolveCurrentWorktree_DanglingSymlink(t *testing.T) {
	hub, worktree := realHub(t, "main")
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	if got, ok := resolveCurrentWorktree(afero.NewOsFs(), hub); ok {
		t.Errorf("resolved %q from a dangling current symlink; want no answer", got)
	}
}

// TestResolveCurrentWorktree_NoCurrentSymlink covers a hub that has never
// been hopped into: hop.json exists, `current` does not.
func TestResolveCurrentWorktree_NoCurrentSymlink(t *testing.T) {
	hub, _ := realHub(t, "main")
	if err := os.Remove(filepath.Join(hub, "current")); err != nil {
		t.Fatalf("remove current: %v", err)
	}

	if got, ok := resolveCurrentWorktree(afero.NewOsFs(), hub); ok {
		t.Errorf("resolved %q from a hub with no current symlink; want no answer", got)
	}
}
