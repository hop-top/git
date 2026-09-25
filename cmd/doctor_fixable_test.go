package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kitcli "hop.top/kit/go/console/cli"

	"hop.top/git/internal/state"
)

// Every issue says whether --fix has a repair for it, and doctor suggests
// --fix only when one does: an issue it cannot repair would still be
// there after --fix, which would exit 1 again.

const (
	summaryFixAll   = "Issues found. Run 'git hop doctor --fix' to automatically repair them."
	summaryFixNone  = "Issues found that 'git hop doctor --fix' cannot repair. Please review the errors and hints above."
	summaryFixSome  = "Issues found. Run 'git hop doctor --fix' to repair 1 of them; for the other 1, review the errors and hints above."
	summaryLeftOver = "Some issues could not be automatically fixed. Please review the errors above."
)

// fixableDoctorEnv is a report with a fixable issue (the data home is
// missing), an unfixable one (state keeps a repository under a key its
// origin no longer gives, because that key is taken), or both.
func fixableDoctorEnv(t *testing.T, fixable, unfixable bool) afero.Fs {
	t.Helper()
	paths := isolateDoctorPaths(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	fs := afero.NewMemMapFs()
	if !fixable {
		require.NoError(t, fs.MkdirAll(paths.dataHome, 0o755))
	}
	if unfixable {
		const origin = "git@gitlab.example.com:acme/widgets.git"
		writeOriginHub(t, fs, "/hubs/old", origin)
		writeOriginHub(t, fs, "/hubs/new", origin)
		st := state.NewState()
		st.AddRepository("github.com/acme/widgets", &state.RepositoryState{URI: origin, Org: "acme", Repo: "widgets",
			Hubs: []*state.HubState{{Path: "/hubs/old", Mode: state.HubModeLocal}}})
		st.AddRepository("gitlab.example.com/acme/widgets", &state.RepositoryState{URI: origin, Org: "acme", Repo: "widgets",
			Hubs: []*state.HubState{{Path: "/hubs/new", Mode: state.HubModeLocal}}})
		require.NoError(t, state.SaveState(fs, st))
	}
	return fs
}

// issueFixability returns each issue record's subject with its fixable
// marker; an issue without one fails the test.
func issueFixability(t *testing.T, r doctorReport) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, rec := range r.records {
		if rec.Kind != doctorKindIssue {
			assert.Nil(t, rec.Fixable, "only issues carry the marker: %+v", rec)
			continue
		}
		require.NotNil(t, rec.Fixable, "issue without a fixable marker: %+v", rec)
		out[rec.Subject] = *rec.Fixable
	}
	return out
}

func TestDoctor_SuggestsFixOnlyForFixableIssues(t *testing.T) {
	for _, tc := range []struct {
		name               string
		fixable, unfixable bool
		want               map[string]bool
		summary            string
	}{
		{"fixable only", true, false, map[string]bool{"/xdg/githop-data": true}, summaryFixAll},
		{"unfixable only", false, true, map[string]bool{"github.com/acme/widgets": false}, summaryFixNone},
		{"mixed", true, true, map[string]bool{"/xdg/githop-data": true, "github.com/acme/widgets": false}, summaryFixSome},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := fixableDoctorEnv(t, tc.fixable, tc.unfixable)
			var r doctorReport
			out := captureStdout(t, func() { r = runDoctor(fs, newRegistryGit(fs), "/elsewhere", doctorOpts{}) })

			assert.Equal(t, tc.want, issueFixability(t, r))
			assert.Equal(t, tc.summary, summaryLine(t, out))
			assert.Error(t, doctorResult(r))
		})
	}
}

// With --fix, doctor says some issues are left exactly when one is: an
// unfixable issue stays, a fixable one is repaired.
func TestDoctorFix_ReportsWhatItCouldNotFix(t *testing.T) {
	for _, tc := range []struct {
		name               string
		fixable, unfixable bool
		want               []string
	}{
		{"fixable only", true, false, []string{"Fixed 1 issue(s)."}},
		{"unfixable only", false, true, []string{summaryLeftOver}},
		{"mixed", true, true, []string{"Fixed 1 issue(s).", summaryLeftOver}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := fixableDoctorEnv(t, tc.fixable, tc.unfixable)
			var r doctorReport
			out := captureStdout(t, func() { r = runDoctor(fs, newRegistryGit(fs), "/elsewhere", doctorOpts{fix: true}) })

			assert.Equal(t, tc.want, summaryLines(out))
			assert.Equal(t, tc.unfixable, doctorResult(r) != nil)
			assert.NotContains(t, out, "Run 'git hop doctor --fix'", "--fix already ran")
		})
	}
}

// The marker reaches the structured result: true or false on an issue,
// absent from every other kind.
func TestDoctorRecord_FixableJSON(t *testing.T) {
	fs := fixableDoctorEnv(t, true, true)
	r := runDoctor(fs, newRegistryGit(fs), "/elsewhere", doctorOpts{fix: true})

	data, err := json.Marshal(r.records)
	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	fixability := map[string]any{}
	for _, rec := range got {
		fixable, ok := rec["fixable"]
		if rec["kind"] != doctorKindIssue {
			assert.False(t, ok, "fixable on a %s record: %s", rec["kind"], data)
			continue
		}
		require.True(t, ok, "issue without fixable: %s", data)
		fixability[rec["subject"].(string)] = fixable
	}
	assert.Equal(t, map[string]any{"/xdg/githop-data": true, "github.com/acme/widgets": false}, fixability)
	assert.Contains(t, doctorSchemaProperties(t), "fixable")
}

// A worktree path something else occupies is fixable only for a merged
// branch, whose row --fix drops; --fix cannot check a worktree out over it.
func TestDoctor_OccupiedWorktreePath_FixableOnlyWhenMerged(t *testing.T) {
	for _, merged := range []bool{false, true} {
		isolateDoctorPaths(t)
		fs := afero.NewMemMapFs()
		hubPath := "/hubs/repo"
		doctorHub(t, fs, hubPath, []string{"main", "feat/file"}, []string{"main"})
		require.NoError(t, afero.WriteFile(fs, worktreeDir(hubPath, "feat/file"), []byte("not a worktree"), 0o644))
		g := newRegistryGit(fs)
		if merged {
			g.Runner.Responses = map[string]string{hubPath + ":git branch --merged main": "  feat/file\n* main\n"}
		}

		r := runDoctor(fs, g, hubPath, doctorOpts{})
		assert.Equal(t, merged, issueFixability(t, r)["feat/file"], "merged=%v records: %+v", merged, r.records)

		r = runDoctor(fs, g, hubPath, doctorOpts{fix: true, dryRun: true})
		assert.Equal(t, merged, len(recordMessages(r, doctorKindWouldFix, "feat/file")) > 0,
			"merged=%v: the marker agrees with what --fix does; records: %+v", merged, r.records)
	}
}

// The summary line doctor printed: the line after "=== Summary ===".
func summaryLine(t *testing.T, out string) string {
	t.Helper()
	lines := summaryLines(out)
	require.Len(t, lines, 1, "output:\n%s", out)
	return lines[0]
}

// summaryLines returns the lines doctor printed after "=== Summary ===".
func summaryLines(out string) []string {
	_, summary, _ := strings.Cut(out, "=== Summary ===\n")
	return strings.Split(strings.TrimRight(summary, "\n"), "\n")
}

// doctorSchemaProperties returns the property names of doctorRecord in the
// output schema doctor publishes.
func doctorSchemaProperties(t *testing.T) []string {
	t.Helper()
	raw, _, ok := kitcli.GetOutputSchemaJSON(doctorCmd)
	require.True(t, ok)
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	var names []string
	for name := range schema.Defs["doctorRecord"].Properties {
		names = append(names, name)
	}
	return names
}
