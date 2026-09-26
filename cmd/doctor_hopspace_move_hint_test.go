package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
)

// renameDeniedFs fails every rename with err, as Windows fails renaming a
// directory with a file open in it.
type renameDeniedFs struct {
	afero.Fs
	err error
}

func (f renameDeniedFs) Rename(_, _ string) error { return f.err }

// A hopspace move that fails on Windows, where renaming a directory whose
// lock files the move holds open is refused, is a failed repair with a
// hint to finish the move by hand. Elsewhere the failure gets no hint.
func TestDoctorFix_HopspaceMoveFailureHintsOnWindows(t *testing.T) {
	denied := &os.LinkError{Op: "rename", Err: errors.New("access is denied")}
	for _, tc := range []struct {
		goos     string
		err      error
		wantHint bool
	}{
		{goos: "windows", err: denied, wantHint: true},
		{goos: "linux", err: denied},
		{goos: "darwin", err: denied},
		// Refusals of the move's own: no lock is to blame.
		{goos: "windows", err: errMovedMeanwhile},
		{goos: "windows", err: &existsError{path: "/elsewhere"}},
	} {
		t.Run(tc.goos+"/"+tc.err.Error(), func(t *testing.T) {
			e := newLockMoveEnv(t)
			fs := renameDeniedFs{Fs: e.fs, err: tc.err}
			prev := doctorGOOS
			doctorGOOS = tc.goos
			t.Cleanup(func() { doctorGOOS = prev })

			var r doctorReport
			repo := &layoutRepo{ref: hop.RepoRef{Org: "acme", Repo: "widgets"}}
			_, stderr := captureRemoveOutput(t, func() {
				reportMisplacedHopspace(fs, doctorOpts{fix: true}, &r, repo, e.alt, e.current)
			})

			require.Len(t, r.failedRecords(), 1, "the move must be a failed repair")
			ok, _ := afero.Exists(e.fs, filepath.Join(e.alt, "ports.json"))
			assert.True(t, ok, "nothing may be lost: the hopspace stays where it was")
			hint := "hint: Close other git-hop processes, then move " + e.alt + " to " + e.current + " by hand."
			if tc.wantHint {
				assert.Contains(t, stderr, hint)
				assert.Contains(t, stderr, "hint: On Windows a directory cannot be renamed while a file in it is open,")
			} else {
				assert.NotContains(t, stderr, "by hand")
			}
		})
	}
}
