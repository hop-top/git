package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/state"
)

// Loading state moves a repository keyed github.com/<org>/<repo> to the
// key its origin gives. When that key is already taken the two entries
// are not merged; doctor reports them as an issue (exit 1), with and
// without --fix, says how to merge them by hand, and leaves state as it
// is.

func writeOriginHub(t *testing.T, fs afero.Fs, hubPath, uri string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"repo": map[string]any{"uri": uri, "org": "acme", "repo": "widgets", "defaultBranch": "main"},
	})
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(hubPath, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, "hop.json"), data, 0o644))
}

func TestDoctor_RepoIDCollision_ReportedAsIssue(t *testing.T) {
	isolateDoctorPaths(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	fs := afero.NewMemMapFs()
	const origin = "git@gitlab.example.com:acme/widgets.git"
	writeOriginHub(t, fs, "/hubs/old", origin)
	writeOriginHub(t, fs, "/hubs/new", origin)

	st := state.NewState()
	st.AddRepository("github.com/acme/widgets", &state.RepositoryState{URI: origin, Org: "acme", Repo: "widgets",
		Hubs: []*state.HubState{{Path: "/hubs/old", Mode: state.HubModeLocal}}})
	st.AddRepository("gitlab.example.com/acme/widgets", &state.RepositoryState{URI: origin, Org: "acme", Repo: "widgets",
		Hubs: []*state.HubState{{Path: "/hubs/new", Mode: state.HubModeLocal}}})
	require.NoError(t, state.SaveState(fs, st))
	before, err := afero.ReadFile(fs, filepath.Join(state.GetStateHome(), "state.json"))
	require.NoError(t, err)

	for _, opts := range []doctorOpts{{}, {fix: true}, {fix: true, dryRun: true}} {
		r := runDoctor(fs, newRegistryGit(fs), "/elsewhere", opts)
		issues := recordMessages(r, doctorKindIssue, "github.com/acme/widgets")
		require.Len(t, issues, 1, "fix=%v dry-run=%v records: %+v", opts.fix, opts.dryRun, r.records)
		assert.Contains(t, issues[0], "/hubs/old")
		assert.Contains(t, issues[0], "gitlab.example.com/acme/widgets")
		assert.Contains(t, issues[0], "merge by hand", "the record says how to resolve it")
		assert.Empty(t, recordMessages(r, doctorKindFixed, "github.com/acme/widgets"), "--fix merges nothing")
		assert.Empty(t, recordMessages(r, doctorKindWouldFix, "github.com/acme/widgets"), "--fix merges nothing")
		assert.Error(t, doctorResult(r), "a collision is an issue: exit 1, with or without --fix")
	}

	after, err := afero.ReadFile(fs, filepath.Join(state.GetStateHome(), "state.json"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "doctor merges nothing")
}
