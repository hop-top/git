package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"hop.top/git/internal/cli"
)

// doctor's exit status, like git fsck's, says whether the installation is
// left healthy: 1 when it reports an issue it did not fix, 0 otherwise.
// Warnings alone never fail it. With --fix it exits 0 only if every issue
// was fixed; --fix --dry-run applies the same rule to the repairs it would
// make.
func TestDoctorExit(t *testing.T) {
	issue := func(check, subject string) doctorRecord {
		return doctorRecord{Kind: doctorKindIssue, Check: check, Subject: subject}
	}
	rec := func(kind, check, subject string) doctorRecord {
		return doctorRecord{Kind: kind, Check: check, Subject: subject}
	}

	for _, tc := range []struct {
		name    string
		opts    doctorOpts
		records []doctorRecord
		want    int
	}{
		{"healthy", doctorOpts{}, nil, 0},
		{"warnings only", doctorOpts{},
			[]doctorRecord{rec(doctorKindWarning, doctorCheckHopspace, "/data/o/r")}, 0},
		{"issue without --fix", doctorOpts{},
			[]doctorRecord{issue(doctorCheckPaths, "/data")}, 1},
		{"--fix fixed every issue", doctorOpts{fix: true}, []doctorRecord{
			issue(doctorCheckPaths, "/data"),
			rec(doctorKindFixed, doctorCheckPaths, "/data"),
			issue(doctorCheckHub, "feat/x"),
			rec(doctorKindFixed, doctorCheckHub, "feat/x"),
		}, 0},
		{"--fix left an issue it has no repair for", doctorOpts{fix: true}, []doctorRecord{
			issue(doctorCheckPaths, "/data"),
			rec(doctorKindFixed, doctorCheckPaths, "/data"),
			issue(doctorCheckHub, "/hub"),
		}, 1},
		{"--fix repair failed", doctorOpts{fix: true}, []doctorRecord{
			issue(doctorCheckPaths, "/data"),
			rec(doctorKindFailed, doctorCheckPaths, "/data"),
		}, 1},
		{"a repair of one subject does not cover another", doctorOpts{fix: true}, []doctorRecord{
			issue(doctorCheckState, "github.com/o/r:a"),
			issue(doctorCheckState, "github.com/o/r:b"),
			rec(doctorKindFixed, doctorCheckState, "github.com/o/r:a"),
			rec(doctorKindFixed, doctorCheckState, "b"),
		}, 1},
		{"dry-run would fix every issue", doctorOpts{fix: true, dryRun: true}, []doctorRecord{
			issue(doctorCheckPaths, "/data"),
			rec(doctorKindWouldFix, doctorCheckPaths, "/data"),
		}, 0},
		{"dry-run cannot fix an issue", doctorOpts{fix: true, dryRun: true}, []doctorRecord{
			issue(doctorCheckHub, "feat/x"),
			rec(doctorKindFailed, doctorCheckHub, "feat/x"),
		}, 1},
		{"dry-run without --fix previews nothing", doctorOpts{dryRun: true},
			[]doctorRecord{issue(doctorCheckPaths, "/data")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := doctorReport{records: tc.records}
			assert.Equal(t, tc.want, cli.ExitCode(doctorResult(r)))
		})
	}
}
