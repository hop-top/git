package hop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/state"
)

// recordHopspace is a hopspace at path recording one branch at recorded.
func recordHopspace(path, recorded string) *Hopspace {
	return &Hopspace{
		Path: path,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"feat/x": {Path: recorded, Exists: true},
			},
		},
	}
}

// TestRecordsWorktree: a record names the worktree however it is
// spelled: relative to the hub or the hopspace, unclean, through a
// symlink, or with the symlink resolved.
func TestRecordsWorktree(t *testing.T) {
	root := t.TempDir()
	hub := filepath.Join(root, "hub")
	target := filepath.Join(hub, "hops", "feat", "x")
	require.NoError(t, os.MkdirAll(target, 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(hub, link))
	resolvedHub, err := filepath.EvalSymlinks(hub)
	require.NoError(t, err)
	dataHome := filepath.Join(root, "data", "org", "repo")
	require.NoError(t, os.MkdirAll(dataHome, 0o755))

	tests := []struct {
		name     string
		hopspace string
		recorded string
		query    string
		want     bool
	}{
		{"absolute, as given", hub, target, target, true},
		{"relative to the hub", hub, "hops/feat/x", target, true},
		{"relative with ./", hub, "./hops/feat/x", target, true},
		{"relative, unclean", hub, "hops/feat/y/../x/", target, true},
		{"absolute, unclean", hub, hub + "/hops/./feat/x/", target, true},
		{"relative to a data-home hopspace", dataHome, "../../../hub/hops/feat/x", target, true},
		{"relative to the hub of a data-home hopspace", dataHome, "hops/feat/x", target, true},
		{"recorded through a symlink", hub, filepath.Join(link, "hops", "feat", "x"), target, true},
		{"queried through a symlink", hub, target, filepath.Join(link, "hops", "feat", "x"), true},
		{"recorded resolved", hub, filepath.Join(resolvedHub, "hops", "feat", "x"), target, true},
		{"missing path, relative", hub, "hops/feat/gone", filepath.Join(hub, "hops", "feat", "gone"), true},
		{"another worktree", hub, "hops/feat/y", target, false},
		{"relative to the working directory only", hub, "feat/x", target, false},
		{"empty record", hub, "", target, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordsWorktree(recordHopspace(tt.hopspace, tt.recorded), hub, tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRecordsWorktree_CaseAsSamePath: case is compared the way the rest
// of git-hop compares paths (state.SamePath), whatever the file system.
func TestRecordsWorktree_CaseAsSamePath(t *testing.T) {
	hub := t.TempDir()
	target := filepath.Join(hub, "hops", "feat", "x")
	require.NoError(t, os.MkdirAll(target, 0o755))
	upper := filepath.Join(hub, "HOPS", "feat", "x")

	got := recordsWorktree(recordHopspace(hub, "HOPS/feat/x"), hub, target)
	assert.Equal(t, state.SamePath(upper, target), got)
}

// TestRecordsWorktree_NoConfig: a hopspace without a config records
// nothing.
func TestRecordsWorktree_NoConfig(t *testing.T) {
	assert.False(t, recordsWorktree(nil, "/hub", "/hub/hops/x"))
	assert.False(t, recordsWorktree(&Hopspace{Path: "/hub"}, "/hub", "/hub/hops/x"))
}

// TestValidateWorktreeAdd_RelativeRecord: a worktree recorded by a
// relative path counts as registered, not as an orphan.
func TestValidateWorktreeAdd_RelativeRecord(t *testing.T) {
	hub := t.TempDir()
	target := filepath.Join(hub, "hops", "feat", "x")
	require.NoError(t, os.MkdirAll(target, 0o755))

	v := NewStateValidator(afero.NewOsFs(), git.New())
	validation, err := v.ValidateWorktreeAdd(recordHopspace(hub, "hops/feat/x"), hub, "feat/x", target)
	require.NoError(t, err)
	assert.True(t, validation.CanProceed)
	assert.Empty(t, validation.Issues)
}

// TestOccupied: only an absent path or an empty directory leaves room
// for a worktree.
func TestOccupied(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/empty", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/full/keep.txt", []byte("x"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/dot/.keep", []byte("x"), 0o644))
	require.NoError(t, fs.MkdirAll("/nested/sub", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/file", []byte("x"), 0o644))

	for path, want := range map[string]bool{
		"/absent": false,
		"/empty":  false,
		"/full":   true,
		"/dot":    true,
		"/nested": true,
		"/file":   true,
	} {
		assert.Equal(t, want, occupied(fs, path), path)
	}
}

// TestCheckAdd_RefusesOccupiedPath: the shared pre-mutation check (the
// preview's too) refuses a non-empty directory, names it and hints at the
// way out; an empty one passes.
func TestCheckAdd_RefusesOccupiedPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := "/hub"
	hopspace := &Hopspace{Path: hub, Config: &config.HopspaceConfig{Branches: map[string]config.HopspaceBranch{}}}
	m := NewWorktreeManager(fs, git.New())

	full := filepath.Join(hub, "hops", "feat", "full")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(full, "keep.txt"), []byte("x"), 0o644))
	err := m.CheckAdd(hopspace, hub, "feat/full", full)
	require.Error(t, err)
	lines := strings.Split(err.Error(), "\n")
	assert.Equal(t, "'"+full+"' already exists and is not an empty directory", lines[0])
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[1], "hint: move it away"), lines[1])

	empty := filepath.Join(hub, "hops", "feat", "empty")
	require.NoError(t, fs.MkdirAll(empty, 0o755))
	assert.NoError(t, m.CheckAdd(hopspace, hub, "feat/empty", empty))

	exists, _ := afero.Exists(fs, filepath.Join(full, "keep.txt"))
	assert.True(t, exists)
}
