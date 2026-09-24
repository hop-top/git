package hop_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

func TestResolveConversionBackupRoot(t *testing.T) {
	root, err := hop.ResolveConversionBackupRoot("")
	require.NoError(t, err)
	assert.Equal(t, hop.DefaultConversionBackupRoot(), root, "unset selects the default root")

	root, err = hop.ResolveConversionBackupRoot("  ")
	require.NoError(t, err)
	assert.Equal(t, hop.DefaultConversionBackupRoot(), root, "blank selects the default root")

	root, err = hop.ResolveConversionBackupRoot("/srv/backups/")
	require.NoError(t, err)
	assert.Equal(t, "/srv/backups", root)

	_, err = hop.ResolveConversionBackupRoot("rel/backups")
	require.Error(t, err, "a relative root is rejected")
	assert.Contains(t, err.Error(), "hop.backup.path")
}

func TestConversionBackupRoots_ConfiguredThenDefault(t *testing.T) {
	def := hop.DefaultConversionBackupRoot()
	assert.Equal(t, []string{"/srv/bk", def}, hop.ConversionBackupRoots("/srv/bk"))
	assert.Equal(t, []string{def}, hop.ConversionBackupRoots(def), "configured == default is listed once")
	assert.Equal(t, []string{def}, hop.ConversionBackupRoots(""))
}

func writeBackup(t *testing.T, fs afero.Fs, dir string, ts time.Time, failed bool) {
	t.Helper()
	writeBackupOf(t, fs, dir, ts, failed, "")
}

func writeBackupOf(t *testing.T, fs afero.Fs, dir string, ts time.Time, failed bool, originalPath string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "original"), 0o755))
	meta := `{"timestamp":"` + ts.Format(time.RFC3339) + `","originalPath":"` + originalPath + `"}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "backup-info.json"), []byte(meta), 0o644))
	if failed {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "conversion-failed"), []byte("x"), 0o644))
	}
}

// Backups under the configured and the default root are listed together,
// newest first, so changing hop.backup.path does not strand old ones.
func TestListConversionBackups_AcrossRoots(t *testing.T) {
	fs := afero.NewMemMapFs()
	// The partial backup has only its directory mtime, which is the real
	// clock; the metadata timestamps are placed around it.
	now := time.Now().Add(time.Hour).Truncate(time.Second)

	cfgDir := hop.ConversionBackupDir("/srv/bk", "acme", "widget")
	defDir := hop.ConversionBackupDir("", "acme", "widget")
	writeBackup(t, fs, filepath.Join(cfgDir, "new"), now, false)
	writeBackup(t, fs, filepath.Join(defDir, "old"), now.Add(-48*time.Hour), true)
	require.NoError(t, fs.MkdirAll(filepath.Join(defDir, "partial", "original"), 0o755))
	// Another repository's backups are not listed.
	writeBackup(t, fs, filepath.Join(hop.ConversionBackupDir("", "acme", "other"), "x"), now, false)

	owner := hop.ConversionBackupOwner{Org: "acme", Repo: "widget"}
	got, err := hop.ListConversionBackups(fs, hop.ConversionBackupRoots("/srv/bk"), owner)
	require.NoError(t, err)
	require.Len(t, got, 3)

	byName := map[string]hop.ConversionBackup{}
	for _, b := range got {
		byName[filepath.Base(b.Path)] = b
	}
	assert.True(t, byName["new"].Complete)
	assert.False(t, byName["new"].Failed)
	assert.True(t, byName["old"].Failed)
	assert.False(t, byName["partial"].Complete, "no metadata yet: incomplete")
	assert.Equal(t, "new", filepath.Base(got[0].Path), "newest first")
}

// A backup belongs to a repository when it copied one of the repository's
// hubs, even from a directory named after another identity: a bare
// conversion re-points origin at the local path, so the hub's identity
// afterwards is not the remote its backup directory was named after.
func TestListConversionBackups_MatchesRecordedHubPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	now := time.Now()
	root := "/srv/bk"
	writeBackupOf(t, fs, filepath.Join(root, "acme-widget", "a"), now, false, "/work/hub")
	writeBackupOf(t, fs, filepath.Join(root, "acme-widget", "b"), now, false, "/work/elsewhere")
	writeBackupOf(t, fs, filepath.Join(root, "work-hub", "c"), now, false, "")

	owner := hop.ConversionBackupOwner{Org: "work", Repo: "hub", HubPaths: []string{"/work/hub"}}
	got, err := hop.ListConversionBackups(fs, []string{root}, owner)
	require.NoError(t, err)
	var names []string
	for _, b := range got {
		names = append(names, filepath.Base(b.Path))
	}
	assert.ElementsMatch(t, []string{"a", "c"}, names, "hub-path match plus own directory; another hub's backup excluded")
}

func TestExpiredConversionBackups(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	b := func(name string, age time.Duration) hop.ConversionBackup {
		return hop.ConversionBackup{Path: name, Time: now.Add(-age), Complete: true}
	}
	// Newest first, as ListConversionBackups returns them.
	all := []hop.ConversionBackup{
		b("d1", 1*day),
		{Path: "partial", Time: now.Add(-2 * day)},
		b("d3", 3*day),
		{Path: "failed", Time: now.Add(-90 * day), Complete: true, Failed: true},
		b("d10", 10*day),
		b("d40", 40*day),
	}
	names := func(bs []hop.ConversionBackup) []string {
		out := []string{}
		for _, x := range bs {
			out = append(out, x.Path)
		}
		return out
	}

	tests := []struct {
		name   string
		policy hop.ConversionBackupRetention
		want   []string
	}{
		{"defaults: count 3, age 30", hop.ConversionBackupRetention{MaxBackups: 3, MaxAgeDays: 30}, []string{"d40"}},
		{"count only", hop.ConversionBackupRetention{MaxBackups: 2}, []string{"d10", "d40"}},
		{"age only", hop.ConversionBackupRetention{MaxAgeDays: 5}, []string{"d10", "d40"}},
		{"count 1 and age 30", hop.ConversionBackupRetention{MaxBackups: 1, MaxAgeDays: 30}, []string{"d3", "d10", "d40"}},
		{"both off", hop.ConversionBackupRetention{}, []string{}},
		{"negative is off", hop.ConversionBackupRetention{MaxBackups: -1, MaxAgeDays: -1}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, names(hop.ExpiredConversionBackups(all, tt.policy, now)))
		})
	}
}

// initPlainRepo makes a committed repo with no remote under a temp dir.
func initPlainRepo(t *testing.T) string {
	t.Helper()
	repoPath := filepath.Join(t.TempDir(), "org", "proj")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	mustRun(t, "git", "init", "-q", "-b", "main", repoPath)
	mustRun(t, "git", "-C", repoPath, "commit", "-q", "--allow-empty", "-m", "init")
	return repoPath
}

// The converter takes its backup under BackupRoot, and a backup kept
// there restores like one in the default location.
func TestConvert_BackupRootHonouredAndRestorable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath := initPlainRepo(t)
	mustRun(t, "git", "-C", repoPath, "remote", "add", "origin", "https://github.com/acme/widget.git")
	root := filepath.Join(t.TempDir(), "bk")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.KeepBackup = true
	conv.BackupRoot = root
	result, err := conv.ConvertToBareWorktree(repoPath, true, true)
	require.NoError(t, err, "errors=%v", result.Errors)

	assert.True(t, strings.HasPrefix(result.BackupPath, filepath.Join(root, "acme-widget")+string(filepath.Separator)),
		"backup %s not under configured root %s", result.BackupPath, root)
	_, err = os.Stat(filepath.Join(result.BackupPath, "backup-info.json"))
	require.NoError(t, err)

	require.NoError(t, conv.RestoreFromBackup(result.BackupPath, repoPath))
	_, err = os.Stat(filepath.Join(repoPath, ".git", "HEAD"))
	assert.NoError(t, err, "restore from the configured root brings back the standard repo")
}

// A backup root inside the repository would copy the repository into
// itself; the conversion refuses before touching anything.
func TestConvert_RefusesBackupRootInsideRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath := initPlainRepo(t)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.BackupRoot = filepath.Join(repoPath, ".backups")
	_, err := conv.ConvertToBareWorktree(repoPath, true, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inside the repository")
	_, statErr := os.Stat(filepath.Join(repoPath, ".backups"))
	assert.True(t, os.IsNotExist(statErr), "nothing written inside the repo")
	_, statErr = os.Stat(filepath.Join(repoPath, ".git"))
	assert.NoError(t, statErr, "repo left as it was")
}

// A failed conversion marks its backup, so retention never removes it.
func TestConvert_FailureMarksBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repoPath := initPlainRepo(t)
	// Occupy the bare clone's destination so the conversion fails after
	// the backup is taken.
	require.NoError(t, os.MkdirAll(repoPath+".new", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath+".new", "blocker"), []byte("x"), 0o644))

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.BackupRoot = filepath.Join(t.TempDir(), "bk")
	result, err := conv.ConvertToBareWorktree(repoPath, true, true)
	require.Error(t, err)
	require.NotEmpty(t, result.BackupPath)

	backups, err := hop.ListConversionBackups(afero.NewOsFs(), []string{conv.BackupRoot}, hop.ConversionBackupOwner{Org: "org", Repo: "proj"})
	require.NoError(t, err)
	require.Len(t, backups, 1)
	assert.True(t, backups[0].Failed, "backup of the failed conversion is marked")
	assert.Empty(t, hop.ExpiredConversionBackups(backups, hop.ConversionBackupRetention{MaxBackups: 1, MaxAgeDays: 1}, time.Now().Add(365*24*time.Hour)))
}
