package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// These tests pin `doctor --fix --dry-run` as a true preview. The bug they
// guard: doctor never read the global --dry-run flag, so a user who asked
// for a preview got the full mutation — dirs created, worktrees recreated,
// orphan dirs deleted, state.json and hop.json rewritten, and a backup
// snapshot taken.
//
// Every assertion is made against doctor's own wiring (runDoctor /
// fixStateIssues), not against the shared prune helpers. Those helpers were
// already dry-run aware; the defect was entirely in doctor's call sites, so
// helper-level tests stay green while the bug is live and prove nothing.

// isolateDoctorPaths redirects every XDG-derived path doctor consults into
// a scratch prefix, so a --fix run in a test can never touch the real
// machine. Paths are returned for assertions.
type doctorPaths struct {
	dataHome   string
	configHome string
	cacheHome  string
	stateHome  string
}

func isolateDoctorPaths(t *testing.T) doctorPaths {
	t.Helper()
	root := "/xdg"
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("GIT_HOP_DATA_HOME", filepath.Join(root, "githop-data"))

	return doctorPaths{
		dataHome:   hop.GetGitHopDataHome(),
		configHome: hop.GetConfigHome(),
		cacheHome:  hop.GetCacheHome(),
		stateHome:  state.GetStateHome(),
	}
}

// TestDoctorDryRun_DoesNotCreateDirectories covers mutation site 1:
// fs.MkdirAll for the data/config/cache directories.
func TestDoctorDryRun_DoesNotCreateDirectories(t *testing.T) {
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()

	r := runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true, dryRun: true})

	assert.True(t, r.issuesFound, "missing dirs are still reported as issues")
	for _, dir := range []string{
		filepath.Join(p.dataHome, "git-hop"),
		filepath.Join(p.configHome, "git-hop"),
		filepath.Join(p.cacheHome, "git-hop"),
	} {
		exists, _ := afero.DirExists(fs, dir)
		assert.False(t, exists, "dry-run must not create %s", dir)
	}
}

// TestDoctorFix_CreatesDirectories is the non-dry-run counterpart: the
// guard must not turn doctor into a command that no longer repairs.
func TestDoctorFix_CreatesDirectories(t *testing.T) {
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()

	runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})

	for _, dir := range []string{
		filepath.Join(p.dataHome, "git-hop"),
		filepath.Join(p.configHome, "git-hop"),
		filepath.Join(p.cacheHome, "git-hop"),
	} {
		exists, _ := afero.DirExists(fs, dir)
		assert.True(t, exists, "--fix must create %s", dir)
	}
}

// doctorHub builds a hub whose hop.json lists branches but where only
// `present` exist on disk, plus a matching hopspace. cwd for runDoctor is
// the hub path itself.
func doctorHub(t *testing.T, fs afero.Fs, hubPath string, branches, present []string) string {
	t.Helper()
	writePruneHub(t, fs, hubPath, branches, present)

	hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(), "test", "repo")
	hopspace, err := hop.InitHopspace(fs, hopspacePath,
		"git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)
	for _, b := range branches {
		require.NoError(t, hopspace.RegisterBranch(b,
			filepath.Join(hubPath, config.MakeWorktreePath(b))))
	}
	return hopspacePath
}

// TestDoctorDryRun_DoesNotRecreateWorktrees covers mutation sites 5-7: the
// parent-dir mkdir, the `git worktree add`, and the hopspace rewrite that
// follows it.
func TestDoctorDryRun_DoesNotRecreateWorktrees(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})

	g := mocks.NewMockGit()
	runDoctor(fs, g, hubPath, doctorOpts{fix: true, dryRun: true})

	assert.Empty(t, g.CreatedWorktrees,
		"dry-run must not run 'git worktree add'")
	gonePath := filepath.Join(hubPath, config.MakeWorktreePath("feat/gone"))
	exists, _ := afero.DirExists(fs, gonePath)
	assert.False(t, exists, "dry-run must not create the worktree dir")
}

// TestDoctorFix_RecreatesWorktrees proves the repair still happens without
// --dry-run.
func TestDoctorFix_RecreatesWorktrees(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})

	g := mocks.NewMockGit()
	runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	gonePath := filepath.Join(hubPath, config.MakeWorktreePath("feat/gone"))
	assert.Contains(t, g.CreatedWorktrees, gonePath,
		"--fix must recreate the missing worktree")
}

// TestDoctorDryRun_DoesNotDeleteOrphanedDirectories covers mutation site 9:
// CleanupOrphanedDirectory's RemoveAll.
func TestDoctorDryRun_DoesNotDeleteOrphanedDirectories(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	hopspacePath := doctorHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	orphan := filepath.Join(hopspacePath, "hops", "orphaned")
	require.NoError(t, fs.MkdirAll(orphan, 0o755))

	runDoctor(fs, mocks.NewMockGit(), hubPath, doctorOpts{fix: true, dryRun: true})

	exists, _ := afero.DirExists(fs, orphan)
	assert.True(t, exists, "dry-run must not delete the orphaned directory")
}

// TestDoctorFix_DeletesOrphanedDirectories is the apply-path counterpart.
func TestDoctorFix_DeletesOrphanedDirectories(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	hopspacePath := doctorHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	orphan := filepath.Join(hopspacePath, "hops", "orphaned")
	require.NoError(t, fs.MkdirAll(orphan, 0o755))

	runDoctor(fs, mocks.NewMockGit(), hubPath, doctorOpts{fix: true})

	exists, _ := afero.DirExists(fs, orphan)
	assert.False(t, exists, "--fix must remove the orphaned directory")
}

// TestDoctorDryRun_DoesNotRewriteHopJSONOrBackup covers mutation site 13:
// the hop.json rewrite and, critically, the RepairBackup snapshot. A
// backup is itself a write — a dry run must not leave a .hop/backups/
// entry behind.
func TestDoctorDryRun_DoesNotRewriteHopJSONOrBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})

	st := stateWithHub(hubPath)

	fixed := fixStateIssues(fs, mocks.NewMockGit(), st, hubPath,
		doctorOpts{fix: true, dryRun: true})

	assert.Equal(t, 1, fixed, "dry-run still reports what would be pruned")
	assert.ElementsMatch(t, []string{"main", "feat/gone"},
		hubBranchKeys(t, fs, hubPath),
		"dry-run must leave hop.json untouched")
	exists, _ := afero.DirExists(fs, filepath.Join(hubPath, ".hop", "backups"))
	assert.False(t, exists,
		"a backup snapshot is a write; dry-run must not take one")
}

// TestDoctorDryRun_DoesNotWriteState covers mutation sites 10-12: the
// in-memory state edits and the state.json save that persists them.
func TestDoctorDryRun_DoesNotWriteState(t *testing.T) {
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"feat/gone": {Path: "/hubs/repo/hops/feat/gone", Type: "linked"},
	}
	require.NoError(t, state.SaveState(fs, st))

	before, err := afero.ReadFile(fs, filepath.Join(p.stateHome, "state.json"))
	require.NoError(t, err)

	loaded, err := state.LoadState(fs)
	require.NoError(t, err)
	fixStateIssues(fs, mocks.NewMockGit(), loaded, hubPath,
		doctorOpts{fix: true, dryRun: true})

	after, err := afero.ReadFile(fs, filepath.Join(p.stateHome, "state.json"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after),
		"dry-run must not rewrite state.json")

	reloaded, err := state.LoadState(fs)
	require.NoError(t, err)
	assert.Contains(t, reloaded.Repositories["github.com/test/repo"].Worktrees,
		"feat/gone", "the orphaned state row must survive a dry run")
}

// TestDoctorFix_WritesState confirms the apply path still prunes state.
func TestDoctorFix_WritesState(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"feat/gone": {Path: "/hubs/repo/hops/feat/gone", Type: "linked"},
	}
	require.NoError(t, state.SaveState(fs, st))

	loaded, err := state.LoadState(fs)
	require.NoError(t, err)
	fixStateIssues(fs, mocks.NewMockGit(), loaded, hubPath, doctorOpts{fix: true})

	reloaded, err := state.LoadState(fs)
	require.NoError(t, err)
	assert.NotContains(t, reloaded.Repositories["github.com/test/repo"].Worktrees,
		"feat/gone", "--fix must prune the orphaned state row")
}

// TestDoctorDryRun_WithoutFixIsInert pins the flag semantics: --dry-run
// alone is a no-op because a run without --fix never mutates anyway, so it
// must behave exactly like a plain diagnostic run.
func TestDoctorDryRun_WithoutFixIsInert(t *testing.T) {
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()

	r := runDoctor(fs, mocks.NewMockGit(), "/nowhere", doctorOpts{dryRun: true})

	assert.True(t, r.issuesFound)
	assert.Zero(t, r.fixed, "no --fix means nothing is even planned")
	exists, _ := afero.DirExists(fs, filepath.Join(p.dataHome, "git-hop"))
	assert.False(t, exists)
}

// TestDoctorOpts_Mutating documents the tri-state the flags encode. Only
// --fix without --dry-run may touch the filesystem.
func TestDoctorOpts_Mutating(t *testing.T) {
	assert.False(t, doctorOpts{}.mutating())
	assert.False(t, doctorOpts{dryRun: true}.mutating())
	assert.True(t, doctorOpts{fix: true}.mutating())
	assert.False(t, doctorOpts{fix: true, dryRun: true}.mutating())

	assert.False(t, doctorOpts{}.planning())
	assert.False(t, doctorOpts{fix: true}.planning())
	assert.True(t, doctorOpts{fix: true, dryRun: true}.planning())
}
