package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// TestDetectRepoStructure_BareRepoAtPath covers the shape that
// cloneBareRepo actually produces: a bare git repo with HEAD/objects/refs
// living directly under the path (no .git subdir). Before the fix,
// DetectRepoStructure returned NotGit for this shape because it only
// looked for <path>/.git, causing all `git hop init` runs against a hop
// hub to bail with "Not in a git repository".
func TestDetectRepoStructure_BareRepoAtPath(t *testing.T) {
	repoPath := t.TempDir()

	// Materialize the on-disk signature of a bare repo at the path root.
	// We don't need real git plumbing — DetectRepoStructure only inspects
	// file/directory presence and HEAD's mode.
	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"),
		[]byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoPath, "objects"), 0755); err != nil {
		t.Fatalf("mkdir objects: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoPath, "refs"), 0755); err != nil {
		t.Fatalf("mkdir refs: %v", err)
	}

	got := hop.DetectRepoStructure(afero.NewOsFs(), repoPath)
	if got != config.BareWorktreeRoot {
		t.Errorf("DetectRepoStructure(bare-at-path) = %v, want %v",
			got, config.BareWorktreeRoot)
	}
}

// TestDetectRepoStructure_NotGit_NoMarkers asserts the negative: a
// directory with no git markers at all still returns NotGit. Guards
// against the bare-at-path detection accidentally over-matching.
func TestDetectRepoStructure_NotGit_NoMarkers(t *testing.T) {
	dir := t.TempDir()
	got := hop.DetectRepoStructure(afero.NewOsFs(), dir)
	if got != config.NotGit {
		t.Errorf("DetectRepoStructure(empty dir) = %v, want %v",
			got, config.NotGit)
	}
}

// TestDetectRepoStructure_NotGit_PartialBareMarkers asserts that having
// some but not all bare-repo markers (e.g. HEAD without objects/) is
// rejected. Prevents false positives on directories that happen to have
// a file called HEAD.
func TestDetectRepoStructure_NotGit_PartialBareMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("noise"), 0644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	got := hop.DetectRepoStructure(afero.NewOsFs(), dir)
	if got != config.NotGit {
		t.Errorf("DetectRepoStructure(HEAD-only) = %v, want %v",
			got, config.NotGit)
	}
}
