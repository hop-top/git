package hop_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	target, err := conv.RestoreToOriginal(backupPath, false)
	require.NoError(t, err)
	assert.Equal(t, repoPath, target)
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
	_, err := conv.RestoreToOriginal(backupPath, false)
	require.NoError(t, err)
	assertStandardRepoAt(t, repoPath)
	entries, err := os.ReadDir(elsewhere)
	require.NoError(t, err)
	assert.Empty(t, entries, "restore wrote into the cwd")
}

// The converted hub still occupies the original location: restore
// refuses and leaves it alone unless told to replace it.
func TestRestoreToOriginal_RefusesNonEmptyTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath, backupPath := convertKeepingBackup(t, filepath.Join(t.TempDir(), "bk"))

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	target, err := conv.RestoreToOriginal(backupPath, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, hop.ErrRestoreTargetNotEmpty), "err = %v", err)
	assert.Equal(t, repoPath, target)
	_, statErr := os.Stat(filepath.Join(repoPath, "hop.json"))
	assert.NoError(t, statErr, "refused restore still touched the hub")

	_, err = conv.RestoreToOriginal(backupPath, true)
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
	_, err := conv.RestoreToOriginal(backupPath, false)
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
	_, err := conv.RestoreToOriginal(inside, true)
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
	_, err := conv.RestoreToOriginal(bk, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "original location")
}
