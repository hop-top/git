package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

func seedConversionBackup(t *testing.T, fs afero.Fs, dir string, ts time.Time, failed bool) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "original"), 0o755))
	meta := `{"timestamp":"` + ts.UTC().Format(time.RFC3339) + `"}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "backup-info.json"), []byte(meta), 0o644))
	if failed {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "conversion-failed"), []byte("x"), 0o644))
	}
}

// Dry run reports the expired backups as would-prune records and removes
// nothing; the real run removes exactly those. The failed conversion's
// backup survives both.
func TestPruneRepoConversionBackups(t *testing.T) {
	fs := afero.NewMemMapFs()
	now := time.Now()
	day := 24 * time.Hour
	roots := hop.ConversionBackupRoots("/srv/bk")
	cfg := hop.ConversionBackupDir("/srv/bk", "acme", "widget")
	def := hop.ConversionBackupDir("", "acme", "widget")
	seedConversionBackup(t, fs, filepath.Join(cfg, "newest"), now.Add(-1*day), false)
	seedConversionBackup(t, fs, filepath.Join(def, "second"), now.Add(-2*day), false)
	seedConversionBackup(t, fs, filepath.Join(def, "stale"), now.Add(-45*day), false)
	seedConversionBackup(t, fs, filepath.Join(def, "failed"), now.Add(-90*day), true)
	policy := hop.ConversionBackupRetention{MaxBackups: 1, MaxAgeDays: 30}

	owner := hop.ConversionBackupOwner{Org: "acme", Repo: "widget"}
	dry := pruneRepoConversionBackups(fs, "github.com/acme/widget", owner, roots, policy, now, true, nil)
	require.Len(t, dry, 2)
	for _, r := range dry {
		assert.Equal(t, "would-prune", r.Action)
		assert.Equal(t, pruneKindConversionBackup, r.Kind)
		assert.Equal(t, "github.com/acme/widget", r.Repository)
		exists, _ := afero.DirExists(fs, r.Path)
		assert.True(t, exists, "dry run removed %s", r.Path)
	}

	real := pruneRepoConversionBackups(fs, "github.com/acme/widget", owner, roots, policy, now, false, nil)
	require.Len(t, real, 2)
	assert.Equal(t, filepath.Join(def, "second"), real[0].Path)
	assert.Equal(t, filepath.Join(def, "stale"), real[1].Path)
	for _, r := range real {
		assert.Equal(t, "pruned", r.Action)
		exists, _ := afero.DirExists(fs, r.Path)
		assert.False(t, exists, "%s not removed", r.Path)
	}
	for _, keep := range []string{filepath.Join(cfg, "newest"), filepath.Join(def, "failed")} {
		exists, _ := afero.DirExists(fs, keep)
		assert.True(t, exists, "%s must survive", keep)
	}
}

func TestConversionBackupOwner(t *testing.T) {
	o := conversionBackupOwner("github.com/x/y", &state.RepositoryState{
		Org: "acme", Repo: "widget", Hubs: []*state.HubState{{Path: "/h1"}, {Path: "/h2"}},
	})
	assert.Equal(t, hop.ConversionBackupOwner{Org: "acme", Repo: "widget", HubPaths: []string{"/h1", "/h2"}}, o, "state entry wins")
	o = conversionBackupOwner("github.com/x/y", &state.RepositoryState{})
	assert.Equal(t, "x", o.Org, "falls back to the repo ID")
	assert.Equal(t, "y", o.Repo)
}

// In an --all sweep a backup two repositories both match is weighed
// against the first one's retention only, and reported once.
func TestPruneRepoConversionBackups_ClaimedOnce(t *testing.T) {
	fs := afero.NewMemMapFs()
	now := time.Now()
	dir := hop.ConversionBackupDir("", "acme", "widget")
	seedConversionBackup(t, fs, filepath.Join(dir, "old"), now.Add(-100*24*time.Hour), false)
	roots := hop.ConversionBackupRoots("")
	policy := hop.ConversionBackupRetention{MaxAgeDays: 30}
	owner := hop.ConversionBackupOwner{Org: "acme", Repo: "widget"}
	claimed := map[string]bool{}

	first := pruneRepoConversionBackups(fs, "github.com/acme/widget", owner, roots, policy, now, true, claimed)
	second := pruneRepoConversionBackups(fs, "github.com/other/widget", owner, roots, policy, now, true, claimed)
	assert.Len(t, first, 1)
	assert.Empty(t, second)
}
