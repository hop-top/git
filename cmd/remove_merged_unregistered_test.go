package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// Worktrees made with plain git (not in hop.json) are judged like
// registered ones: a branch rebase-merged into origin/<default> while the
// local default is behind is a merged candidate, with origin/<default> as
// its target. A branch in neither ref is not a candidate.
func TestCollectMergedCandidates_UnregisteredRebaseMergedIntoOrigin(t *testing.T) {
	dir := staleLocalDefaultRepo(t)
	// Free the hub checkout from feat so feat and other can have worktrees.
	gitRun(t, dir, "checkout", "-q", "main")
	wtRoot := t.TempDir()
	featPath := filepath.Join(wtRoot, "feat")
	otherPath := filepath.Join(wtRoot, "other")
	gitRun(t, dir, "worktree", "add", featPath, "feat")
	gitRun(t, dir, "worktree", "add", otherPath, "other")

	hub := &hop.Hub{Path: dir, Config: remoteOnlyDefaultHub("next")}
	toRemove, skipped := collectMergedCandidates(afero.NewOsFs(), git.New(), hub, dir, "/nowhere", "")

	require.Len(t, toRemove, 1, "only the rebase-merged worktree is a candidate")
	assert.Empty(t, skipped)
	c := toRemove[0]
	assert.True(t, c.Unregistered)
	assert.Equal(t, "feat", c.GitBranch)
	assert.Equal(t, featPath, c.WorktreePath)
	assert.Equal(t, "origin/next", c.Target.Branch)
	assert.Equal(t, compareSourceDefault, c.Target.Source)
}
