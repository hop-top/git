package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/shell"
)

// Clone is the very first worktree a user ever gets, and until it lands in
// the roots cache a plain `cd` into it is invisible to the shell handler.
// That is the fresh-install case: the feature reads as doing nothing at all.
//
// The refresh cannot live inside hop.CloneWorktree -- internal/shell imports
// internal/hop, so the reverse import would cycle -- so it is wired here, at
// the caller, right after the clone returns.
func TestClonePath_RefreshesRootsCache(t *testing.T) {
	src, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("read root.go: %v", err)
	}
	text := string(src)

	cloneAt := strings.Index(text, "hop.CloneWorktree(")
	if cloneAt == -1 {
		t.Fatal("clone path no longer calls hop.CloneWorktree")
	}

	// Search only the clone branch, not the rest of the file: the helper's
	// own definition lives further down and would otherwise satisfy a
	// naive scan whether or not anything actually calls it.
	branch := text[cloneAt:]
	if end := strings.Index(branch, "\n\t\t\treturn\n"); end != -1 {
		branch = branch[:end]
	}
	if !strings.Contains(branch, "refreshRootsCacheAt(") {
		t.Error("clone never refreshes the worktree-roots cache; a freshly " +
			"cloned worktree stays invisible to plain `cd`")
	}
}

// refreshRootsCacheAt loads the hub at a path and rebuilds its cache
// entries. It is what the clone path uses, since clone has no *hop.Hub in
// hand -- it has just written one to disk.
func TestRefreshRootsCacheAt_RegistersAFreshlyClonedWorktree(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewMemMapFs()

	hubPath := filepath.FromSlash("/w/fresh")
	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:o/r.git", "o", "r", "main")
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	mainPath := filepath.Join(hubPath, "hops", "main")
	if err := hub.AddBranch("main", "main", mainPath); err != nil {
		t.Fatalf("add branch: %v", err)
	}

	refreshRootsCacheAt(fs, hubPath)

	if got := shell.ReadRootsCache(fs); shell.LookupRoot(got, mainPath) != mainPath {
		t.Errorf("clone refresh did not register %q: %v", mainPath, got)
	}
}

// A hub that cannot be loaded is not an error the user should ever see: the
// clone itself already succeeded, and the cache is an optimisation.
func TestRefreshRootsCacheAt_MissingHubIsSilent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	refreshRootsCacheAt(afero.NewMemMapFs(), filepath.FromSlash("/nowhere"))
}
