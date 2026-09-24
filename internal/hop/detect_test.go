package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
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

	got := hop.DetectRepoStructure(afero.NewOsFs(), git.New(), repoPath)
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
	got := hop.DetectRepoStructure(afero.NewOsFs(), git.New(), dir)
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
	got := hop.DetectRepoStructure(afero.NewOsFs(), git.New(), dir)
	if got != config.NotGit {
		t.Errorf("DetectRepoStructure(HEAD-only) = %v, want %v",
			got, config.NotGit)
	}
}

// TestDetectRepoStructure_GitAuthoritative classifies real repositories by
// what git reports (is-bare-repository, git dir vs common dir), not by
// which admin directories happen to exist. A non-bare repository grows a
// .git/worktrees/ directory as soon as it has one linked worktree; that
// makes it neither bare nor a git-hop hub.
func TestDetectRepoStructure_GitAuthoritative(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	g := git.New()

	regular := func(t *testing.T, name string) string {
		t.Helper()
		r := filepath.Join(dir, name)
		mustRun(t, "git", "init", "-q", "-b", "main", r)
		mustRun(t, "git", "-C", r, "commit", "-q", "--allow-empty", "-m", "init")
		return r
	}

	cases := []struct {
		name  string
		setup func(t *testing.T) string
		want  config.StructureType
	}{
		{"regular repo, no linked worktree", func(t *testing.T) string {
			return regular(t, "plain")
		}, config.StandardRepo},
		{"regular repo with one linked worktree", func(t *testing.T) string {
			r := regular(t, "p")
			mustRun(t, "git", "-C", r, "worktree", "add", "-q", filepath.Join(dir, "p-feat"), "-b", "feat")
			return r
		}, config.StandardRepo},
		{"linked worktree of a regular repo", func(t *testing.T) string {
			r := regular(t, "q")
			wt := filepath.Join(dir, "q-feat")
			mustRun(t, "git", "-C", r, "worktree", "add", "-q", wt, "-b", "feat")
			return wt
		}, config.WorktreeChild},
		{"bare hub (git hop layout)", func(t *testing.T) string {
			return bareHub(t, dir, "hub")
		}, config.BareWorktreeRoot},
		{"worktree of a bare hub", func(t *testing.T) string {
			return filepath.Join(bareHub(t, dir, "hub2"), "hops", "main")
		}, config.WorktreeChild},
		{"bare repository kept in .git, with a linked worktree", func(t *testing.T) string {
			r := filepath.Join(dir, "dotbare")
			mustRun(t, "git", "clone", "-q", "--bare", regular(t, "dotbare-src"), filepath.Join(r, ".git"))
			mustRun(t, "git", "-C", r, "worktree", "add", "-q", "hops/main", "main")
			return r
		}, config.BareWorktreeRoot},
		{"regular repo registered as a hub (hop.json)", func(t *testing.T) string {
			r := regular(t, "reghub")
			mustRun(t, "git", "-C", r, "worktree", "add", "-q", filepath.Join(dir, "reghub-feat"), "-b", "feat")
			if err := os.WriteFile(filepath.Join(r, "hop.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			return r
		}, config.WorktreeRoot},
		{"repository with a separate git dir (a .git file, not a worktree)", func(t *testing.T) string {
			r := filepath.Join(dir, "sep")
			mustRun(t, "git", "init", "-q", "--separate-git-dir", filepath.Join(dir, "sep.git"), r)
			return r
		}, config.UnknownStructure},
		{"the .git dir of a regular repo", func(t *testing.T) string {
			return filepath.Join(regular(t, "inside"), ".git")
		}, config.UnknownStructure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t)
			if got := hop.DetectRepoStructure(fs, g, path); got != tc.want {
				t.Fatalf("DetectRepoStructure(%s) = %v, want %v", path, got, tc.want)
			}
		})
	}
}
