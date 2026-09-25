package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A missing worktree doctor cannot recreate is left in place: its hop.json
// row survives the run, since dropping it would lose the only record of
// the unmerged branch it pointed at. And a --dry-run promises a recreate
// only when nothing a real run checks would stop it: when something would,
// the preview reports the same failure the real run does.

// failingAddGit is a registryGit whose `git worktree add` fails for a
// reason no precondition foresees.
type failingAddGit struct{ *registryGit }

func (g failingAddGit) CreateWorktree(base, branch, path, start string, forceCreate bool, track string) error {
	g.calls = append(g.calls, "add "+path)
	return errors.New("fatal: could not create work tree dir")
}

// TestDoctorFix_FailedRecreate_KeepsHopJSONRow: state has no entry for
// the branch, so no prompt ever runs for it; the hop.json pass used to
// drop the row of the worktree the hub check had just failed to recreate.
func TestDoctorFix_FailedRecreate_KeepsHopJSONRow(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
	gone := worktreeDir(hubPath, "feat/gone")

	g := failingAddGit{newRegistryGit(fs)}
	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.NotEmpty(t, recordMessages(r, doctorKindFailed, "feat/gone"), "the failed recreate is reported: %+v", r.records)
	assert.Error(t, doctorResult(r))
	assert.Contains(t, hubBranchKeys(t, fs, hubPath), "feat/gone", "a row doctor could not repair is kept")
	assert.NotContains(t, strings.Join(recordMessages(r, doctorKindFixed, "feat/gone"), "\n"), "prune hop.json entry")
	exists, _ := afero.DirExists(fs, gone)
	assert.False(t, exists)
}

// TestDoctor_RecreatePreconditions: for each thing that makes `git
// worktree add` refuse, the preview and the real run report the same
// failure, neither runs git worktree add, and both keep the hop.json row.
func TestDoctor_RecreatePreconditions(t *testing.T) {
	const hubPath = "/hubs/repo"
	gone := worktreeDir(hubPath, "feat/gone")
	stale := "prunable gitdir file points to non-existent location"

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, fs afero.Fs, g *registryGit, hopspacePath string)
		want  string // in the failed record's message
	}{
		{
			name: "branch missing",
			setup: func(t *testing.T, fs afero.Fs, g *registryGit, hopspacePath string) {
				// Asked in the hub's repository: the hub is --global, and
				// its data-home hopspace is no repository at all.
				g.Runner.Errors = map[string]error{
					hubPath + ":git rev-parse --verify --quiet refs/heads/feat/gone^{commit}": errors.New("exit 1"),
					hubPath + ":git rev-parse --verify --quiet origin/feat/gone^{commit}":     errors.New("exit 1"),
				}
			},
			want: "branch feat/gone does not exist",
		},
		{
			name: "branch checked out elsewhere",
			setup: func(t *testing.T, fs afero.Fs, g *registryGit, hopspacePath string) {
				g.WorktreeListOut = porcelainEntry("/elsewhere/feat-gone", "feat/gone")
			},
			want: "already checked out at /elsewhere/feat-gone",
		},
		{
			name: "branch held by another stale registration",
			setup: func(t *testing.T, fs afero.Fs, g *registryGit, hopspacePath string) {
				g.WorktreeListOut = porcelainEntry("/elsewhere/feat-gone", "feat/gone", stale)
			},
			want: "already checked out at /elsewhere/feat-gone",
		},
		{
			name: "registration git will not prune",
			setup: func(t *testing.T, fs afero.Fs, g *registryGit, hopspacePath string) {
				g.WorktreeListOut = porcelainEntry(gone, "feat/gone")
			},
			want: "git still registers " + gone,
		},
		{
			name: "path occupied",
			setup: func(t *testing.T, fs afero.Fs, g *registryGit, hopspacePath string) {
				// hops/feat is a file, so hops/feat/gone cannot be created.
				require.NoError(t, afero.WriteFile(fs, filepath.Dir(gone), []byte("x"), 0o644))
			},
			want: filepath.Dir(gone) + " is in the way",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []string
			for _, opts := range []doctorOpts{{fix: true, dryRun: true}, {fix: true}} {
				isolateDoctorPaths(t)
				fs := afero.NewMemMapFs()
				hopspacePath := doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
				g := newRegistryGit(fs)
				tc.setup(t, fs, g, hopspacePath)

				r := runDoctor(fs, g, hubPath, opts)

				failed := recordMessages(r, doctorKindFailed, "feat/gone")
				require.Len(t, failed, 1, "dry-run=%v: %+v", opts.dryRun, r.records)
				assert.Contains(t, failed[0], "cannot recreate worktree")
				assert.Contains(t, failed[0], tc.want)
				messages = append(messages, failed[0])

				assert.Empty(t, recordMessages(r, doctorKindWouldFix, "feat/gone"), "dry-run=%v: no repair is promised", opts.dryRun)
				assert.Empty(t, recordMessages(r, doctorKindFixed, "feat/gone"), "dry-run=%v: nothing is repaired", opts.dryRun)
				assert.Error(t, doctorResult(r), "dry-run=%v", opts.dryRun)
				assert.Empty(t, g.calls, "dry-run=%v: git worktree add/remove never runs", opts.dryRun)
				assert.Contains(t, hubBranchKeys(t, fs, hubPath), "feat/gone", "dry-run=%v: the row is kept", opts.dryRun)
				exists, _ := afero.DirExists(fs, gone)
				assert.False(t, exists)
			}
			assert.Equal(t, messages[0], messages[1], "the preview and the real run report the same failure")
		})
	}
}

// TestRecreateBlocker_Locked: a locked registration at the path blocks a
// recreate and names git's reason. The hub check never gets here for a
// locked worktree (it warns and moves on), so this is the helper alone.
func TestRecreateBlocker_Locked(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	gone := worktreeDir("/hubs/repo", "feat/gone")
	g := newRegistryGit(fs)

	blocker := recreateBlocker(fs, g, porcelainEntry(gone, "feat/gone", "locked on the usb drive"), "/hubs/repo", "feat/gone", gone)

	assert.Contains(t, blocker, "locked")
	assert.Contains(t, blocker, "on the usb drive")
}

// TestRecreateBlocker_None guards the inverse direction: a stale
// registration at the path itself (cleared before the add) and a branch
// that exists block nothing.
func TestRecreateBlocker_None(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	gone := worktreeDir("/hubs/repo", "feat/gone")
	g := newRegistryGit(fs)

	registry := porcelainEntry(gone, "feat/gone", "prunable gitdir file points to non-existent location") +
		porcelainEntry("/hubs/repo/hops/main", "main")
	assert.Empty(t, recreateBlocker(fs, g, registry, "/hubs/repo", "feat/gone", gone))
}
