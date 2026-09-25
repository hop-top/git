package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A regular file where a hub branch's worktree directory should be used
// to pass the hub check as present, while the state check called the
// same path missing. The hub check now reports it, as a path in the way.
func TestDoctor_FileAtWorktreePath_ReportedByHubCheck(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/file"}, []string{"main"})
	occupied := worktreeDir(hubPath, "feat/file")
	require.NoError(t, afero.WriteFile(fs, occupied, []byte("not a worktree"), 0o644))

	r := runDoctor(fs, newRegistryGit(fs), hubPath, doctorOpts{})

	issues := recordMessages(r, doctorKindIssue, "feat/file")
	require.Len(t, issues, 1, "records: %+v", r.records)
	assert.Contains(t, issues[0], "occupied by a non-directory")
	assert.Contains(t, issues[0], occupied)
	assert.Error(t, doctorResult(r))
}

// --fix must not try to check a worktree out over the file, nor drop the
// branch's hop.json row: the failed repair leaves it for the user, and
// the preview reports the same verdict.
func TestDoctorFix_FileAtWorktreePath_NotRecreatedOver(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "fix"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			isolateDoctorPaths(t)
			fs := afero.NewMemMapFs()
			hubPath := "/hubs/repo"
			doctorHub(t, fs, hubPath, []string{"main", "feat/file"}, []string{"main"})
			occupied := worktreeDir(hubPath, "feat/file")
			require.NoError(t, afero.WriteFile(fs, occupied, []byte("not a worktree"), 0o644))

			g := newRegistryGit(fs)
			r := runDoctor(fs, g, hubPath, doctorOpts{fix: true, dryRun: dryRun})

			assert.Empty(t, g.calls, "nothing may be checked out over the file")
			failed := strings.Join(recordMessages(r, doctorKindFailed, "feat/file"), "\n")
			assert.Contains(t, failed, occupied+" is in the way")
			assert.Empty(t, recordMessages(r, doctorKindWouldFix, "feat/file"))
			assert.Empty(t, recordMessages(r, doctorKindFixed, "feat/file"))
			assert.Error(t, doctorResult(r))

			data, err := afero.ReadFile(fs, occupied)
			require.NoError(t, err)
			assert.Equal(t, "not a worktree", string(data), "the file is left alone")
			assert.Contains(t, hubBranchKeys(t, fs, hubPath), "feat/file",
				"the row of a worktree doctor could not restore is kept")
		})
	}
}
