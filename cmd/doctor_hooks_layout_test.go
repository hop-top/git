package cmd

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// legacyHooksEnv is a data home holding hooks where releases before
// hop.dataLayout mirrored them, <data>/github.com/acme/widgets/hooks.
type legacyHooksEnv struct {
	fs             afero.Fs
	legacy, newDir string
}

func newLegacyHooksEnv(t *testing.T) legacyHooksEnv {
	t.Helper()
	p := isolateDoctorPaths(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(p.dataHome, 0o755))
	e := legacyHooksEnv{
		fs:     fs,
		legacy: filepath.Join(p.dataHome, "github.com", "acme", "widgets", "hooks"),
		newDir: filepath.Join(p.dataHome, "acme", "widgets", "hooks"),
	}
	writeTestFile(t, fs, filepath.Join(e.legacy, "post-worktree-add"), "#!/bin/sh\necho old\n")
	return e
}

func writeTestFile(t *testing.T, fs afero.Fs, path, body string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o755))
}

func legacyHooksRecords(r doctorReport, subject string) []doctorRecord {
	var out []doctorRecord
	for _, rec := range r.records {
		if rec.Check == doctorCheckHopspace && rec.Subject == subject {
			out = append(out, rec)
		}
	}
	return out
}

func TestDoctor_WarnsAboutLegacyHooksDir(t *testing.T) {
	e := newLegacyHooksEnv(t)

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{})

	recs := legacyHooksRecords(r, e.legacy)
	require.Len(t, recs, 1, "want one record for the old hooks dir; got %+v", r.records)
	assert.Equal(t, doctorKindWarning, recs[0].Kind)
	assert.Contains(t, recs[0].Message, e.newDir)
	assert.Contains(t, recs[0].Message, "git hop doctor --fix")
	assert.NoError(t, doctorResult(r), "old-location hooks still fire; a warning must not fail doctor")

	ok, _ := afero.Exists(e.fs, filepath.Join(e.legacy, "post-worktree-add"))
	assert.True(t, ok, "doctor without --fix must not move anything")
}

func TestDoctorFix_MovesLegacyHooksWhenNewLocationFree(t *testing.T) {
	e := newLegacyHooksEnv(t)

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})

	body, err := afero.ReadFile(e.fs, filepath.Join(e.newDir, "post-worktree-add"))
	require.NoError(t, err, "hooks must be at the new location after --fix")
	assert.Equal(t, "#!/bin/sh\necho old\n", string(body))
	gone, _ := afero.DirExists(e.fs, e.legacy)
	assert.False(t, gone, "the old hooks dir is moved, not copied")

	recs := legacyHooksRecords(r, e.legacy)
	require.NotEmpty(t, recs)
	assert.Equal(t, doctorKindFixed, recs[len(recs)-1].Kind)
	assert.NoError(t, doctorResult(r))
}

func TestDoctorFixDryRun_LegacyHooksIsNoOp(t *testing.T) {
	e := newLegacyHooksEnv(t)

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true, dryRun: true})

	ok, _ := afero.Exists(e.fs, filepath.Join(e.legacy, "post-worktree-add"))
	assert.True(t, ok, "dry-run must leave the old hooks in place")
	created, _ := afero.DirExists(e.fs, e.newDir)
	assert.False(t, created, "dry-run must not create the new hooks dir")

	recs := legacyHooksRecords(r, e.legacy)
	require.NotEmpty(t, recs)
	assert.Equal(t, doctorKindWouldFix, recs[len(recs)-1].Kind)
}

// With hooks at both locations nothing is moved or overwritten, even with
// --fix; doctor reports and leaves the choice to the user.
func TestDoctorFix_LegacyAndNewHooksConflictUntouched(t *testing.T) {
	e := newLegacyHooksEnv(t)
	writeTestFile(t, e.fs, filepath.Join(e.newDir, "post-worktree-add"), "#!/bin/sh\necho new\n")

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})

	oldBody, err := afero.ReadFile(e.fs, filepath.Join(e.legacy, "post-worktree-add"))
	require.NoError(t, err, "the old hook must survive")
	assert.Equal(t, "#!/bin/sh\necho old\n", string(oldBody))
	newBody, err := afero.ReadFile(e.fs, filepath.Join(e.newDir, "post-worktree-add"))
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\necho new\n", string(newBody), "the new hook must not be overwritten")

	recs := legacyHooksRecords(r, e.legacy)
	require.Len(t, recs, 1, "want one report and no repair; got %+v", r.records)
	assert.Equal(t, doctorKindWarning, recs[0].Kind)
	assert.Contains(t, recs[0].Message, "different content")
}

// Under {host}/{org}/{repo}, a github.com repository's old hooks dir is its
// new one: nothing to report.
func TestDoctor_HostLayoutGitHubHooksAreCurrent(t *testing.T) {
	e := newLegacyHooksEnv(t)
	out, err := exec.Command("git", "config", "--global", "hop.dataLayout", "{host}/{org}/{repo}").CombinedOutput()
	require.NoError(t, err, string(out))

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{fix: true})

	assert.Empty(t, legacyHooksRecords(r, e.legacy))
	ok, _ := afero.Exists(e.fs, filepath.Join(e.legacy, "post-worktree-add"))
	assert.True(t, ok)
}

func TestDoctor_WarnsAboutInvalidDataLayout(t *testing.T) {
	e := newLegacyHooksEnv(t)
	out, err := exec.Command("git", "config", "--global", "hop.dataLayout", "{repo}").CombinedOutput()
	require.NoError(t, err, string(out))

	r := runDoctor(e.fs, mocks.NewMockGit(), "/nowhere", doctorOpts{})

	var found bool
	for _, rec := range r.records {
		if rec.Check == doctorCheckConfig && rec.Subject == "hop.dataLayout" {
			found = true
			assert.Equal(t, doctorKindWarning, rec.Kind)
		}
	}
	assert.True(t, found, "want a config warning for the invalid hop.dataLayout; got %+v", r.records)
}

// gitRepoDir creates a real bare repository whose local config holds kv:
// hop.dataLayout is read with git, from disk, whatever fs the test uses.
func gitRepoDir(t *testing.T, kv map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	out, err := exec.Command("git", "init", "-q", "--bare", dir).CombinedOutput()
	require.NoError(t, err, string(out))
	for k, v := range kv {
		out, err := exec.Command("git", "-C", dir, "config", k, v).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	return dir
}

// A repository-level hop.dataLayout git-hop cannot use is reported with
// its scope and the layout used instead (the --global one).
func TestDoctor_WarnsAboutInvalidRepoDataLayout(t *testing.T) {
	e := newLegacyHooksEnv(t)
	out, err := exec.Command("git", "config", "--global", "hop.dataLayout", "{host}/{org}/{repo}").CombinedOutput()
	require.NoError(t, err, string(out))
	hubPath := gitRepoDir(t, map[string]string{"hop.dataLayout": "{repo}"})
	_, err = hop.CreateHub(e.fs, hubPath, "git@github.com:acme/widgets.git", "acme", "widgets", "main")
	require.NoError(t, err)

	r := runDoctor(e.fs, mocks.NewMockGit(), hubPath, doctorOpts{})

	var msgs []string
	for _, rec := range r.records {
		if rec.Check == doctorCheckConfig && rec.Subject == "hop.dataLayout" {
			assert.Equal(t, doctorKindWarning, rec.Kind)
			msgs = append(msgs, rec.Message)
		}
	}
	require.Len(t, msgs, 1, "want one warning, for the repository value; got %+v", r.records)
	assert.Contains(t, msgs[0], "local config")
	assert.Contains(t, msgs[0], "using {host}/{org}/{repo}")
}
