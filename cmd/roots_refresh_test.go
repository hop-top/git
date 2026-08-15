package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/shell"
)

// The shell integration's worktree-roots cache is what lets a plain `cd`
// into a worktree be detected without forking on every prompt. It is only
// as good as the last command that refreshed it: a worktree created,
// removed, or renamed without a refresh is invisible to the handler for the
// rest of the session, and the feature silently does nothing.
//
// Every command that changes WHICH worktrees exist therefore has to refresh
// it. These tests pin that wiring.

// refreshingCommands lists the mutating command files and the hub variable
// each one holds at the point the refresh belongs.
var refreshingCommands = []struct {
	name string
	file string
}{
	{"add", "add.go"},
	{"remove", "remove.go"},
	{"move", "move.go"},
}

// A source-level assertion is the right shape here: the failure this guards
// against is a refresh call being dropped, and no amount of behavioural
// testing of the surrounding command reaches that. The commands' Run
// closures do real filesystem and git work end to end, so calling them is
// not a viable substitute.
func TestMutatingCommands_RefreshRootsCache(t *testing.T) {
	for _, tc := range refreshingCommands {
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			if !strings.Contains(string(src), "refreshRootsCache(") {
				t.Errorf("%s never refreshes the worktree-roots cache; "+
					"worktrees it touches stay invisible to plain `cd`", tc.file)
			}
		})
	}
}

// refreshRootsCache is best-effort by contract: the underlying operation has
// already succeeded by the time it runs, and a cache write must never turn a
// successful add/remove/move into a failure. It therefore reports nothing
// upward and tolerates every degenerate input a command path can hold.
func TestRefreshRootsCache_ToleratesADegenerateHub(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	// A nil hub is what a command holds when hop.json could not be loaded.
	refreshRootsCache(fs, nil, filepath.FromSlash("/w/hub"))

	// A hub with no config at all -- same contract.
	refreshRootsCache(fs, &hop.Hub{Path: filepath.FromSlash("/w/hub")}, filepath.FromSlash("/w/hub"))
}

// The refresh must reflect the hub's branch set as it stands AFTER the
// mutation, including a set that shrank. This is the case the merge-only
// cache could not express at all.
func TestRefreshRootsCache_ReflectsAShrunkBranchSet(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	memfs := afero.NewMemMapFs()
	hubPath := filepath.FromSlash("/w/hub")
	hub, err := hop.CreateHub(memfs, hubPath, "git@github.com:o/r.git", "o", "r", "main")
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	mainPath := filepath.Join(hubPath, "hops", "main")
	featPath := filepath.Join(hubPath, "hops", "feature")
	if err := hub.AddBranch("main", "main", mainPath); err != nil {
		t.Fatalf("add main: %v", err)
	}
	if err := hub.AddBranch("feature", "feature", featPath); err != nil {
		t.Fatalf("add feature: %v", err)
	}

	refreshRootsCache(fs, hub, hubPath)
	if got := shell.ReadRootsCache(fs); shell.LookupRoot(got, featPath) != featPath {
		t.Fatalf("refresh did not register the new worktree: %v", got)
	}

	// Mirror what removeBranchWorktree does to the in-memory hub.
	delete(hub.Config.Branches, "feature")
	refreshRootsCache(fs, hub, hubPath)

	got := shell.ReadRootsCache(fs)
	if shell.LookupRoot(got, featPath) != "" {
		t.Errorf("removed worktree survived the refresh: %v", got)
	}
	if shell.LookupRoot(got, mainPath) != mainPath {
		t.Errorf("refresh dropped a worktree that still exists: %v", got)
	}
}
