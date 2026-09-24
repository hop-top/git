package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

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
		recordMessages(r, doctorKindWouldFix, "github.com/test/repo:feat/gone"))
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
