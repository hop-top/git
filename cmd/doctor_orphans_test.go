package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/test/mocks"
)

// doctor --fix removes an orphaned directory under hops/ only when it is
// empty. A worktree git has registered, and any directory with something
// in it, is reported for the user to handle and survives --fix and
// --fix --dry-run alike, every file in it untouched.

// orphanFixture lays out, under the hopspace's hops/, one orphaned
// directory of each kind beside the recorded main worktree:
//
//	wip/        a worktree git has registered, with uncommitted work
//	feat/x/     one git has registered below an unrecorded parent
//	plain/      a plain directory with a file in it
//	empty/      an empty directory
type orphanFixture struct {
	fs    afero.Fs
	g     *mocks.MockGit
	hub   string
	hops  string
	files []string // every file that must survive
}

func newOrphanFixture(t *testing.T) orphanFixture {
	t.Helper()
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hub := "/hubs/repo"
	hops := filepath.Join(doctorHub(t, fs, hub, []string{"main"}, []string{"main"}), "hops")

	f := orphanFixture{fs: fs, hub: hub, hops: hops}
	for _, rel := range []string{"wip/.git", "wip/uncommitted.txt", "feat/x/.git", "feat/x/wip.txt", "plain/keep.txt"} {
		p := filepath.Join(hops, rel)
		require.NoError(t, fs.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, afero.WriteFile(fs, p, []byte("work in progress"), 0o644))
		f.files = append(f.files, p)
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(hops, "empty"), 0o755))

	f.g = mocks.NewMockGit()
	f.g.WorktreeListOut = "worktree " + hub + "\nbare\n\n" +
		"worktree " + filepath.Join(hops, "main") + "\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree " + filepath.Join(hops, "wip") + "\nHEAD abc\nbranch refs/heads/wip\n\n" +
		"worktree " + filepath.Join(hops, "feat", "x") + "\nHEAD abc\nbranch refs/heads/feat/x\n"
	return f
}

// assertSurvived fails for every file of the fixture that is gone or
// changed.
func (f orphanFixture) assertSurvived(t *testing.T) {
	t.Helper()
	for _, p := range f.files {
		got, err := afero.ReadFile(f.fs, p)
		if assert.NoError(t, err, "%s must survive", p) {
			assert.Equal(t, "work in progress", string(got), "%s must be unchanged", p)
		}
	}
}

// orphanRecords maps each worktrees-check issue subject to whether it is
// fixable, and each fixed or would-fix record's subject to its kind.
// messages holds each issue's message.
func orphanRecords(r doctorReport) (issues map[string]bool, repairs, messages map[string]string) {
	issues, repairs, messages = map[string]bool{}, map[string]string{}, map[string]string{}
	for _, rec := range r.records {
		if rec.Check != doctorCheckWorktrees {
			continue
		}
		switch rec.Kind {
		case doctorKindIssue:
			issues[rec.Subject] = *rec.Fixable
			messages[rec.Subject] = rec.Message
		case doctorKindFixed, doctorKindWouldFix, doctorKindFailed:
			repairs[rec.Subject] = rec.Kind
		}
	}
	return issues, repairs, messages
}

func TestDoctorOrphans_OnlyEmptyDirectoryRemoved(t *testing.T) {
	for _, tc := range []struct {
		name       string
		opts       doctorOpts
		emptyGone  bool
		repairKind string
	}{
		{"report", doctorOpts{}, false, ""},
		{"fix dry-run", doctorOpts{fix: true, dryRun: true}, false, doctorKindWouldFix},
		{"fix", doctorOpts{fix: true}, true, doctorKindFixed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOrphanFixture(t)

			r := runDoctor(f.fs, f.g, f.hub, tc.opts)

			f.assertSurvived(t)
			for _, dir := range []string{"wip", "feat/x", "plain"} {
				exists, _ := afero.DirExists(f.fs, filepath.Join(f.hops, dir))
				assert.True(t, exists, "%s must survive", dir)
			}
			exists, _ := afero.DirExists(f.fs, filepath.Join(f.hops, "empty"))
			assert.Equal(t, !tc.emptyGone, exists, "empty directory present")

			issues, repairs, messages := orphanRecords(r)
			assert.Equal(t, map[string]bool{
				filepath.Join(f.hops, "wip"):   false,
				filepath.Join(f.hops, "feat"):  false,
				filepath.Join(f.hops, "plain"): false,
				filepath.Join(f.hops, "empty"): true,
			}, issues)
			assert.Contains(t, messages[filepath.Join(f.hops, "wip")], "git has registered ("+filepath.Join(f.hops, "wip")+")")
			assert.Contains(t, messages[filepath.Join(f.hops, "feat")], "git has registered ("+filepath.Join(f.hops, "feat", "x")+")")
			assert.Contains(t, messages[filepath.Join(f.hops, "plain")], "not empty")
			want := map[string]string{}
			if tc.repairKind != "" {
				want[filepath.Join(f.hops, "empty")] = tc.repairKind
			}
			assert.Equal(t, want, repairs, "only the empty directory is (or would be) removed")
			assert.Error(t, doctorResult(r), "what doctor leaves behind keeps the exit status 1")
		})
	}
}

// An unreadable worktree list takes no directory for a worktree, but
// still removes only the empty one.
func TestDoctorOrphans_UnreadableWorktreeList(t *testing.T) {
	f := newOrphanFixture(t)
	f.g.WorktreeListErr = assert.AnError

	runDoctor(f.fs, f.g, f.hub, doctorOpts{fix: true})

	f.assertSurvived(t)
	exists, _ := afero.DirExists(f.fs, filepath.Join(f.hops, "empty"))
	assert.False(t, exists, "the empty directory is removed")
}

// A registration whose directory is gone holds no work: the empty
// directory it was under is removed like any other.
func TestDoctorOrphans_MissingRegisteredWorktreeHoldsNothing(t *testing.T) {
	f := newOrphanFixture(t)
	stale := filepath.Join(f.hops, "stale")
	require.NoError(t, f.fs.MkdirAll(stale, 0o755))
	f.g.WorktreeListOut += "\nworktree " + filepath.Join(stale, "gone") + "\nHEAD abc\nbranch refs/heads/stale/gone\nprunable gitdir file points to non-existent location\n"

	r := runDoctor(f.fs, f.g, f.hub, doctorOpts{fix: true})

	f.assertSurvived(t)
	issues, _, _ := orphanRecords(r)
	assert.True(t, issues[stale], "the empty directory is fixable")
	exists, _ := afero.DirExists(f.fs, stale)
	assert.False(t, exists, "the empty directory is removed")
}
