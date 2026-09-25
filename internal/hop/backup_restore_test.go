package hop_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// convertKeepingBackup runs a bare conversion of a no-remote repository
// with its backup kept under root, and returns the repo and backup paths.
func convertKeepingBackup(t *testing.T, root string) (repoPath, backupPath string) {
	t.Helper()
	repoPath = initPlainRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "keep.txt"), []byte("original\n"), 0o644))
	mustRun(t, "git", "-C", repoPath, "add", "keep.txt")
	mustRun(t, "git", "-C", repoPath, "commit", "-q", "-m", "keep")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.KeepBackup = true
	conv.BackupRoot = root
	result, err := conv.ConvertToBareWorktree(repoPath, true, true)
	require.NoError(t, err, "errors=%v", result.Errors)
	return repoPath, result.BackupPath
}

func assertStandardRepoAt(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(path, ".git"))
	require.NoError(t, err, "no .git at %s", path)
	assert.True(t, info.IsDir(), "%s/.git is not a directory: not the standard repo", path)
	data, err := os.ReadFile(filepath.Join(path, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "original\n", string(data))
	_, err = os.Stat(filepath.Join(path, "hop.json"))
	assert.True(t, os.IsNotExist(err), "hop.json of the converted hub survived the restore")
}

// A repository without a remote records an empty remote URL; restore
// does not need org/repo and must load such a backup.
func TestRestoreToOriginal_NoRemoteBackupRestores(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	require.NoError(t, os.RemoveAll(repoPath))

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	res, err := conv.RestoreToOriginal(backupPath, hop.RestoreOptions{})
	require.NoError(t, err)
	assert.Equal(t, repoPath, res.Target)
	assert.Empty(t, res.MovedAside, "nothing was there to move aside")
	assertStandardRepoAt(t, repoPath)
}

// The target is the location the backup was taken from, whatever the
// process cwd is.
func TestRestoreToOriginal_IgnoresCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	require.NoError(t, os.RemoveAll(repoPath))

	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	_, err := conv.RestoreToOriginal(backupPath, hop.RestoreOptions{})
	require.NoError(t, err)
	assertStandardRepoAt(t, repoPath)
	entries, err := os.ReadDir(elsewhere)
	require.NoError(t, err)
	assert.Empty(t, entries, "restore wrote into the cwd")
}

// The converted hub still occupies the original location: restore
// refuses and leaves it alone unless told to move it aside.
func TestRestoreToOriginal_RefusesNonEmptyTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	res, err := conv.RestoreToOriginal(backupPath, hop.RestoreOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, hop.ErrRestoreTargetNotEmpty), "err = %v", err)
	assert.Equal(t, repoPath, res.Target)
	_, statErr := os.Stat(filepath.Join(repoPath, "hop.json"))
	assert.NoError(t, statErr, "refused restore still touched the hub")

	_, err = conv.RestoreToOriginal(backupPath, force)
	require.NoError(t, err)
	assertStandardRepoAt(t, repoPath)
}

// An existing but empty directory holds nothing to lose.
func TestRestoreToOriginal_EmptyTargetRestores(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	require.NoError(t, os.RemoveAll(repoPath))
	require.NoError(t, os.MkdirAll(repoPath, 0o755))

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	_, err := conv.RestoreToOriginal(backupPath, hop.RestoreOptions{})
	require.NoError(t, err)
	assertStandardRepoAt(t, repoPath)
}

// Replacing the target would delete a backup that lives inside it, and
// with it the only copy being restored.
func TestRestoreToOriginal_RefusesBackupInsideTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	inside := filepath.Join(repoPath, "backup-copy")
	mustRun(t, "cp", "-R", backupPath, inside)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	_, err := conv.RestoreToOriginal(inside, force)
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(inside, "backup-info.json"))
	assert.NoError(t, statErr, "backup inside the target was deleted")
}

// Metadata without the original location gives restore nowhere to go;
// it must not fall back to some other directory.
func TestRestoreToOriginal_RequiresRecordedLocation(t *testing.T) {
	fs := afero.NewMemMapFs()
	bk := "/bk/org-repo/2026-01-01_00-00-00"
	require.NoError(t, fs.MkdirAll(filepath.Join(bk, "original", ".git"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(bk, "backup-info.json"), []byte(`{"remoteUrl":""}`), 0o644))

	conv := hop.NewConverter(fs, git.New())
	_, err := conv.RestoreToOriginal(bk, force)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "original location")
}

// fixedClock pins the moved-aside timestamp so names are predictable and
// two restores can land in the same second.
func fixedClock() time.Time { return time.Date(2026, 9, 24, 10, 15, 0, 0, time.UTC) }

// force is init --restore --force, on the fixed clock.
var force = hop.RestoreOptions{Replace: true, Clock: fixedClock}

// treeSnapshot maps every path under root (relative) to its kind and
// content, so a tree can be compared with where it was moved.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			dest, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "link:" + dest
		case d.IsDir():
			snap[rel] = "dir"
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = "file:" + string(data)
		}
		return nil
	})
	require.NoError(t, err)
	return snap
}

// --force never deletes what occupies the original location: the whole
// tree, including a worktree added after the conversion and untracked
// work in it, moves to <path>.pre-restore-<UTC time>, and the backup is
// restored into the freed path.
func TestRestoreToOriginal_ForceMovesOccupantAside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	mustRun(t, "git", "-C", repoPath, "worktree", "add", "-q", filepath.Join(repoPath, "hops", "feat"), "-b", "feat")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "hops", "feat", "wip.txt"), []byte("unsaved work\n"), 0o644))
	before := treeSnapshot(t, repoPath)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	res, err := conv.RestoreToOriginal(backupPath, force)
	require.NoError(t, err)

	assert.Equal(t, repoPath+".pre-restore-20260924T101500Z", res.MovedAside)
	assert.Equal(t, before, treeSnapshot(t, res.MovedAside), "moved-aside tree differs from what occupied the location")
	assertStandardRepoAt(t, repoPath)
}

// Two restores in the same second must not collide on the moved-aside
// name: the second gets a numeric suffix and the first stays intact.
func TestRestoreToOriginal_SameSecondDistinctNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	conv := hop.NewConverter(afero.NewOsFs(), git.New())

	first, err := conv.RestoreToOriginal(backupPath, force)
	require.NoError(t, err)
	firstSnap := treeSnapshot(t, first.MovedAside)
	second, err := conv.RestoreToOriginal(backupPath, force)
	require.NoError(t, err)

	assert.Equal(t, repoPath+".pre-restore-20260924T101500Z", first.MovedAside)
	assert.Equal(t, repoPath+".pre-restore-20260924T101500Z-1", second.MovedAside)
	assert.Equal(t, firstSnap, treeSnapshot(t, first.MovedAside), "second restore disturbed the first moved-aside tree")
	_, err = os.Stat(filepath.Join(first.MovedAside, "hop.json"))
	assert.NoError(t, err, "first moved-aside tree lost the hub")
	_, err = os.Stat(filepath.Join(second.MovedAside, ".git", "HEAD"))
	assert.NoError(t, err, "second moved-aside tree is not the first restore's repo")
	assertStandardRepoAt(t, repoPath)
}

// When the occupant cannot be moved aside, restore stops before touching
// anything: the location keeps its contents and nothing is created.
func TestRestoreToOriginal_FailedMoveTouchesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	before := treeSnapshot(t, repoPath)
	parent := filepath.Dir(repoPath)
	require.NoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	res, err := conv.RestoreToOriginal(backupPath, force)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "move")
	assert.Empty(t, res.MovedAside)

	require.NoError(t, os.Chmod(parent, 0o755))
	assert.Equal(t, before, treeSnapshot(t, repoPath), "failed restore changed the location")
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), "pre-restore", "failed restore left %s behind", e.Name())
	}
}

// A dry run over an occupied location reports the move a real run would
// make, to the name it would use, and moves and restores nothing.
func TestRestoreToOriginal_DryRunOccupiedChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	parent := filepath.Dir(repoPath)
	before := treeSnapshot(t, parent)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.DryRun = true
	res, err := conv.RestoreToOriginal(backupPath, force)
	require.NoError(t, err)

	assert.Equal(t, repoPath, res.Target)
	assert.True(t, res.Occupied)
	assert.Equal(t, repoPath+".pre-restore-20260924T101500Z", res.MovedAside)
	assert.Equal(t, before, treeSnapshot(t, parent), "dry run changed the location or its parent")
}

// A dry run to a missing location plans no move and creates nothing.
func TestRestoreToOriginal_DryRunMissingTargetChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	require.NoError(t, os.RemoveAll(repoPath))

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.DryRun = true
	res, err := conv.RestoreToOriginal(backupPath, hop.RestoreOptions{})
	require.NoError(t, err)

	assert.False(t, res.Occupied)
	assert.Empty(t, res.MovedAside)
	_, err = os.Lstat(repoPath)
	assert.True(t, os.IsNotExist(err), "dry run created %s", repoPath)
}

// A dry run refuses what a real run refuses, with the same error.
func TestRestoreToOriginal_DryRunRefusesLikeARealRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	before := treeSnapshot(t, repoPath)

	for _, dry := range []bool{true, false} {
		conv := hop.NewConverter(afero.NewOsFs(), git.New())
		conv.DryRun = dry
		_, err := conv.RestoreToOriginal(backupPath, hop.RestoreOptions{})
		require.Error(t, err, "dry=%v", dry)
		assert.True(t, errors.Is(err, hop.ErrRestoreTargetNotEmpty), "dry=%v: err = %v", dry, err)
	}
	assert.Equal(t, before, treeSnapshot(t, repoPath))
}

// A backup whose copy of the repository is gone is refused before the
// occupant is moved aside, on a real run as on a dry run: there would be
// nothing to put in its place.
func TestRestoreToOriginal_MissingCopyRefusedBeforeMoving(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))
	require.NoError(t, os.Rename(filepath.Join(backupPath, "original"), filepath.Join(backupPath, "gone")))
	parent := filepath.Dir(repoPath)
	before := treeSnapshot(t, parent)

	for _, dry := range []bool{true, false} {
		conv := hop.NewConverter(afero.NewOsFs(), git.New())
		conv.DryRun = dry
		res, err := conv.RestoreToOriginal(backupPath, force)
		require.Error(t, err, "dry=%v", dry)
		assert.Contains(t, err.Error(), "backup not found", "dry=%v", dry)
		assert.Empty(t, res.MovedAside, "dry=%v", dry)
	}
	assert.Equal(t, before, treeSnapshot(t, parent), "refused restore moved the occupant")
}
