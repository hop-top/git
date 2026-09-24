package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// TestDoctorDryRun_StateEntryRepairedOnce: a merged missing worktree's
// state entry gets one would-fix, not a "remove entry" and then a second
// "prune worktree entry" for the same entry, and is counted once.
func TestDoctorDryRun_StateEntryRepairedOnce(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main"}, []string{"main"})

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"/hubs/repo/hops/feat/gone": {Path: "/hubs/repo/hops/feat/gone", Branch: "feat/gone", Type: "linked", HubPath: hubPath},
	}
	g := mocks.NewMockGit()
	g.Runner.Responses = map[string]string{hubPath + ":git branch --merged main": "  feat/gone\n* main\n"}

	var r doctorReport
	fixed := fixStateIssues(fs, g, st, hubPath, nil, doctorOpts{fix: true, dryRun: true}, &r)

	assert.Equal(t, 1, fixed)
	assert.Equal(t, []string{"remove entry: branch is merged into main"},
		recordMessages(r, doctorKindWouldFix, "/hubs/repo/hops/feat/gone"))
}

// TestDedupeRepairs: a second repair recorded for the same check and
// subject is dropped and uncounted; issues, failures, and repairs of
// other subjects are kept.
func TestDedupeRepairs(t *testing.T) {
	in := []doctorRecord{
		{Kind: doctorKindIssue, Check: doctorCheckState, Subject: "r:a", Message: "worktree missing"},
		{Kind: doctorKindWouldFix, Check: doctorCheckState, Subject: "r:a", Message: "remove entry"},
		{Kind: doctorKindWouldFix, Check: doctorCheckState, Subject: "r:a", Message: "prune worktree entry from state"},
		{Kind: doctorKindWouldFix, Check: doctorCheckHub, Subject: "r:a", Message: "another check"},
		{Kind: doctorKindWouldFix, Check: doctorCheckState, Subject: "r:b", Message: "another subject"},
		{Kind: doctorKindFailed, Check: doctorCheckState, Subject: "r:a", Message: "failed"},
	}

	out, dropped := dedupeRepairs(in)

	assert.Equal(t, 1, dropped)
	assert.Equal(t, append(append([]doctorRecord{}, in[:2]...), in[3:]...), out)
}

// Two hubs of one repository can each miss their worktree of the same
// branch. Each is its own finding and its own repair: doctor names state
// findings by worktree path, so the second is neither deduplicated away
// nor paired with the first one's repair.
func TestDoctorFix_TwoHubsMissingSameBranch_RepairedSeparately(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	st := stateWithHub("/hubs/a")
	require.NoError(t, st.AddHub(registerRepoID, &state.HubState{Path: "/hubs/b"}))
	for _, hub := range []string{"/hubs/a", "/hubs/b"} {
		require.NoError(t, fs.MkdirAll(hub, 0o755))
		require.NoError(t, st.PutWorktree(registerRepoID, &state.WorktreeState{
			Path: hub + "/hops/feat", Branch: "feat", Type: "linked", HubPath: hub,
		}))
	}
	g := mocks.NewMockGit()
	for _, hub := range []string{"/hubs/a", "/hubs/b"} {
		g.Runner.Responses[hub+":git branch --merged main"] = "  feat\n* main\n"
	}

	require.NoError(t, state.SaveState(fs, st))

	var r doctorReport
	loaded, issues := inspectState(fs, g, &r)
	require.Len(t, issues, 2)
	fixed := fixStateIssues(fs, g, loaded, "", nil, doctorOpts{fix: true, dryRun: true}, &r)

	assert.Equal(t, 2, fixed, "one repair per hub's worktree")
	for _, path := range []string{"/hubs/a/hops/feat", "/hubs/b/hops/feat"} {
		assert.Len(t, recordMessages(r, doctorKindIssue, path), 1, path)
		assert.Equal(t, []string{"remove entry: branch is merged into main"}, recordMessages(r, doctorKindWouldFix, path), path)
	}
	assert.NoError(t, doctorResult(r), "records: %+v", r.records)
}
