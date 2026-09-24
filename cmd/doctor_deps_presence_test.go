package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A regular file at one branch's worktree path used to abort the whole
// dependency audit (detecting package managers under it fails with "not
// a directory"), so no worktree's dependencies were checked. The file is
// skipped; the other worktrees are still audited. Run on the real
// filesystem: the in-memory one reports a path below a file as missing
// rather than failing, which hides the bug.
func TestDoctorDependencies_FileAtWorktreePath_AuditsTheRest(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for key, dir := range map[string]string{
		"XDG_DATA_HOME":     "data",
		"XDG_CONFIG_HOME":   "config",
		"XDG_CACHE_HOME":    "cache",
		"XDG_STATE_HOME":    "state",
		"GIT_HOP_DATA_HOME": "githop-data",
	} {
		t.Setenv(key, filepath.Join(root, dir))
	}

	fs := afero.NewOsFs()
	hubPath := filepath.Join(root, "hub")
	writePruneHub(t, fs, hubPath, []string{"main", "feat/file"}, []string{"main"})
	occupied := worktreeDir(hubPath, "feat/file")
	require.NoError(t, os.MkdirAll(filepath.Dir(occupied), 0o755))
	require.NoError(t, os.WriteFile(occupied, []byte("not a worktree"), 0o644))

	// main keeps its own node_modules instead of the shared symlink: an
	// issue the audit reports only if it gets to main.
	mainPath := worktreeDir(hubPath, "main")
	require.NoError(t, os.WriteFile(filepath.Join(mainPath, "package.json"), []byte(`{"name":"x"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(mainPath, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(mainPath, "node_modules", "left-pad"), 0o755))

	r := doctorReport{records: []doctorRecord{}}
	checkDependencies(fs, hubPath, doctorOpts{}, &r)

	for _, rec := range r.records {
		assert.NotContains(t, rec.Message, "failed to audit", "the audit must not abort: %+v", rec)
	}
	mainIssues := strings.Join(recordMessages(r, doctorKindIssue, "main"), "\n")
	assert.Contains(t, mainIssues, "local node_modules", "records: %+v", r.records)
	assert.Empty(t, recordMessages(r, doctorKindIssue, "feat/file"),
		"the hub check reports the occupied path; the dependency check only skips it")
}
