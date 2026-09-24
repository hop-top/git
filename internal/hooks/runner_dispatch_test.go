package hooks

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var envHookNames = []string{"pre-env-start", "post-env-start", "pre-env-stop", "post-env-stop"}

func TestIsDispatched_EnvHooksAreReserved(t *testing.T) {
	for _, name := range envHookNames {
		assert.False(t, IsDispatched(name), "%s is never dispatched", name)
		assert.NoError(t, ValidateHookName(name), "%s stays a valid name", name)
	}
}

func TestRepoLevelHookNames(t *testing.T) {
	got := RepoLevelHookNames()

	for _, name := range envHookNames {
		assert.NotContains(t, got, name)
	}
	assert.NotContains(t, got, "pre-clone", "pre-clone has no repo level")

	for _, name := range []string{
		"pre-worktree-add", "post-worktree-add",
		"pre-worktree-remove", "post-worktree-remove",
		"pre-worktree-move", "post-worktree-move",
		"pre-repair", "post-repair",
		"post-clone",
		"pre-worktree-switch", "post-worktree-switch",
	} {
		assert.Contains(t, got, name)
	}
}

// TestIsDispatched_MatchesCallSites keeps the dispatched/reserved split
// honest against the code: every dispatched name appears as a string
// literal somewhere outside this package, and no reserved name does.
func TestIsDispatched_MatchesCallSites(t *testing.T) {
	root := filepath.Join("..", "..")
	var src strings.Builder
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == filepath.Join(root, "internal", "hooks") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			src.Write(b)
			return nil
		})
		require.NoError(t, err)
	}
	code := src.String()

	for _, name := range ValidHookNames {
		literal := `"` + name + `"`
		if IsDispatched(name) {
			assert.Contains(t, code, literal, "%s is marked dispatched but no call site names it", name)
		} else {
			assert.NotContains(t, code, literal, "%s is marked reserved but code names it", name)
		}
	}
}

// notInWorktreeDir are the repo-level hooks whose dispatcher never starts
// the lookup at an existing worktree: pre-worktree-add runs before the
// worktree exists, post-worktree-remove after it is gone, and repair
// anchors on the hub. A script in a worktree's own .git-hop/hooks/ is
// never found for them.
var notInWorktreeDir = []string{"pre-worktree-add", "post-worktree-remove", "pre-repair", "post-repair"}

func TestWorktreeLevelHookNames_ExcludesHooksNotAnchoredOnAWorktree(t *testing.T) {
	got := WorktreeLevelHookNames()
	for _, name := range notInWorktreeDir {
		assert.NotContains(t, got, name)
	}
	for _, name := range []string{
		"post-worktree-add", "pre-worktree-remove",
		"pre-worktree-move", "post-worktree-move",
		"post-clone",
		"pre-worktree-switch", "post-worktree-switch",
	} {
		assert.Contains(t, got, name)
	}
}

// Every repo-level hook resolves from a hub-level .git-hop/hooks/: either
// it anchors there (repair) or the parent walk from a worktree under the
// hub reaches it. The two lists partition RepoLevelHookNames.
func TestHubOnlyHookNames_PartitionRepoLevel(t *testing.T) {
	assert.ElementsMatch(t, notInWorktreeDir, HubOnlyHookNames())
	assert.ElementsMatch(t, RepoLevelHookNames(), append(WorktreeLevelHookNames(), HubOnlyHookNames()...))
}

// The repair anchor is the hub, so a repair hook in a worktree's own dir
// is not found even though the worktree sits under the hub.
func TestFindHookFile_RepairFromHubMissesWorktreeDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	wtHook := filepath.Join("/hub", "hops", "main", ".git-hop", "hooks", "pre-repair")
	require.NoError(t, afero.WriteFile(fs, wtHook, []byte("#!/bin/sh\n"), 0o755))

	r := NewRunner(fs)
	assert.Empty(t, r.FindHookFile("pre-repair", "/hub", ""))

	hubHook := filepath.Join("/hub", ".git-hop", "hooks", "pre-repair")
	require.NoError(t, afero.WriteFile(fs, hubHook, []byte("#!/bin/sh\n"), 0o755))
	assert.Equal(t, hubHook, r.FindHookFile("pre-repair", "/hub", ""))
}
