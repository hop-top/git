package cmd

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

func TestPlanBranchDeletion(t *testing.T) {
	newGit := func() *mocks.MockGit {
		g := mocks.NewMockGit()
		g.LocalBranches = []string{"feat/a"}
		return g
	}

	t.Run("existing branch goes", func(t *testing.T) {
		o := planBranchDeletion(newGit(), "/hub", "feat/a", "main", removeOptions{})
		assert.True(t, o.Delete)
		assert.Empty(t, o.KeptReason)
	})
	t.Run("absent branch is reported, not a failure", func(t *testing.T) {
		o := planBranchDeletion(newGit(), "/hub", "feat/zzz", "main", removeOptions{})
		assert.False(t, o.Delete)
		assert.False(t, o.Failed)
		assert.Contains(t, o.KeptReason, "absent")
	})
	// A probe git could not run is not "already absent": that read hid a
	// branch left behind.
	t.Run("failed probe is a failure, not absent", func(t *testing.T) {
		g := newGit()
		g.BranchExistsErr = errors.New("git command failed: git [for-each-ref]: exit status 128 (stderr: fatal: not a git repository)")
		o := planBranchDeletion(g, "/hub", "feat/a", "main", removeOptions{})
		assert.False(t, o.Delete)
		assert.True(t, o.Failed)
		assert.NotContains(t, o.KeptReason, "absent")
		assert.Contains(t, o.KeptReason, "not a git repository")
	})
	t.Run("keep wins and names its source", func(t *testing.T) {
		o := planBranchDeletion(newGit(), "/hub", "feat/a", "main", removeOptions{KeepReason: "--keep-branch"})
		assert.False(t, o.Delete)
		assert.Equal(t, "kept by --keep-branch", o.KeptReason)
	})
	t.Run("default branch is never deleted", func(t *testing.T) {
		g := newGit()
		g.LocalBranches = append(g.LocalBranches, "main")
		o := planBranchDeletion(g, "/hub", "main", "main", removeOptions{})
		assert.False(t, o.Delete)
		assert.Contains(t, o.KeptReason, "default branch")
	})
	t.Run("detached worktree has no branch", func(t *testing.T) {
		o := planBranchDeletion(newGit(), "/hub", "", "main", removeOptions{})
		assert.False(t, o.Delete)
		assert.NotEmpty(t, o.KeptReason)
	})
}

func TestResolveWorktreeBranch(t *testing.T) {
	g := mocks.NewMockGit()
	g.WorktreeListOut = "worktree /hub\nbare\n\n" +
		"worktree /hub/hops/feat/a\nHEAD abc\nbranch refs/heads/other\n\n" +
		"worktree /hub/hops/det\nHEAD abc\ndetached\n"

	assert.Equal(t, "other", resolveWorktreeBranch(g, "/hub", "/hub/hops/feat/a", "feat/a"))
	assert.Equal(t, "det-key", resolveWorktreeBranch(g, "/hub", "/hub/hops/det", "det-key"), "detached: fall back to the key")
	assert.Equal(t, "unlisted", resolveWorktreeBranch(g, "/hub", "/hub/hops/unlisted", "unlisted"))

	g.WorktreeListErr = errors.New("boom")
	assert.Equal(t, "feat/a", resolveWorktreeBranch(g, "/hub", "/hub/hops/feat/a", "feat/a"))
}

func TestResolveRemoveKeepBranch(t *testing.T) {
	newCmd := func(argv ...string) *cobra.Command {
		c := &cobra.Command{Use: "remove"}
		c.Flags().Bool("keep-branch", false, "")
		require.NoError(t, c.ParseFlags(argv))
		return c
	}
	on := stubGitConfig(map[string]string{config.KeyRemoveKeepBranch: "true"})
	off := stubGitConfig(nil)

	assert.Empty(t, resolveRemoveKeepBranch(newCmd(), off))
	assert.Equal(t, "--keep-branch", resolveRemoveKeepBranch(newCmd("--keep-branch"), off))
	assert.Equal(t, config.KeyRemoveKeepBranch, resolveRemoveKeepBranch(newCmd(), on))
	assert.Empty(t, resolveRemoveKeepBranch(newCmd("--keep-branch=false"), on), "typed flag beats config")
	assert.Equal(t, "hop.remove.keepBranch", config.KeyRemoveKeepBranch)
}

func TestRemoveCmd_HasKeepBranchFlag(t *testing.T) {
	f := removeCmd.Flags().Lookup("keep-branch")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}
