package cmd

import (
	"errors"
	"strings"
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/test/mocks"
)

// TestRemoveGate covers the four safety cases plus their flag-bypass
// combinations. The matrix mirrors the documented spec on `git hop
// remove`: unmerged+unpushed needs --force --no-verify, unmerged needs
// --force, merged+dirty needs --no-verify, and merged+clean is silent.
func TestRemoveGate(t *testing.T) {
	cases := []struct {
		name      string
		safety    branchSafety
		force     bool
		noVerify  bool
		wantErr   bool
		errSubstr string
	}{
		// Case 1: unmerged + unpushed.
		{"unmerged unpushed: no flags", branchSafety{}, false, false, true, "--force --no-verify"},
		{"unmerged unpushed: only force", branchSafety{}, true, false, true, "--force --no-verify"},
		{"unmerged unpushed: only no-verify", branchSafety{}, false, true, true, "--force --no-verify"},
		{"unmerged unpushed: both flags", branchSafety{}, true, true, false, ""},

		// Case 2: unmerged + pushed.
		{"unmerged pushed: no flags", branchSafety{Pushed: true, Clean: true}, false, false, true, "not merged"},
		{"unmerged pushed: only no-verify", branchSafety{Pushed: true, Clean: true}, false, true, true, "not merged"},
		{"unmerged pushed: only force", branchSafety{Pushed: true, Clean: true}, true, false, false, ""},
		{"unmerged pushed: both flags", branchSafety{Pushed: true, Clean: true}, true, true, false, ""},

		// Case 3: merged + dirty (Pushed irrelevant).
		{"merged dirty: no flags", branchSafety{Merged: true}, false, false, true, "uncommitted"},
		{"merged dirty: only force", branchSafety{Merged: true}, true, false, true, "uncommitted"},
		{"merged dirty: only no-verify", branchSafety{Merged: true}, false, true, false, ""},
		{"merged dirty pushed: only no-verify", branchSafety{Merged: true, Pushed: true}, false, true, false, ""},

		// Case 4: merged + clean — silent pass with or without flags.
		{"merged clean: no flags", branchSafety{Merged: true, Clean: true}, false, false, false, ""},
		{"merged clean pushed: no flags", branchSafety{Merged: true, Pushed: true, Clean: true}, false, false, false, ""},
		{"merged clean: extra flags", branchSafety{Merged: true, Clean: true}, true, true, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := removeGate(tc.safety, tc.force, tc.noVerify)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
			}
		})
	}
}

// TestInspectBranchSafety verifies the probe wires the right git
// commands and interprets their output correctly. The mock keys
// responses by "<dir>:git <args>" so the test pins the exact commands
// the gate relies on.
func TestInspectBranchSafety(t *testing.T) {
	const (
		dir    = "/wt"
		branch = "feature"
		def    = "main"
	)

	// Command keys used by inspectBranchSafety, in the order it calls
	// them. The MockCommandRunner builds keys as "<dir>:<cmd> <args>".
	mergedKey := dir + ":git rev-list --count " + branch + " --not " + def
	verifyOriginKey := dir + ":git rev-parse --verify refs/remotes/origin/" + branch
	pushedKey := dir + ":git rev-list --count " + branch + " --not refs/remotes/origin/" + branch

	cleanStatus := &git.Status{Branch: branch, Clean: true}
	dirtyStatus := &git.Status{Branch: branch, Clean: false, Files: []string{"? untracked.txt"}}

	cases := []struct {
		name      string
		responses map[string]string
		errs      map[string]error
		status    *git.Status
		want      branchSafety
	}{
		{
			name: "fully merged, fully pushed, clean",
			responses: map[string]string{
				mergedKey:       "0",
				verifyOriginKey: "deadbeef",
				pushedKey:       "0",
			},
			status: cleanStatus,
			want:   branchSafety{Merged: true, Pushed: true, Clean: true},
		},
		{
			name: "ahead of default and remote, clean",
			responses: map[string]string{
				mergedKey:       "3",
				verifyOriginKey: "deadbeef",
				pushedKey:       "2",
			},
			status: cleanStatus,
			want:   branchSafety{Clean: true},
		},
		{
			name: "no remote tracking branch",
			responses: map[string]string{
				mergedKey: "1",
			},
			errs: map[string]error{
				verifyOriginKey: errors.New("unknown ref"),
			},
			status: cleanStatus,
			want:   branchSafety{Clean: true},
		},
		{
			name: "merged but dirty",
			responses: map[string]string{
				mergedKey:       "0",
				verifyOriginKey: "deadbeef",
				pushedKey:       "0",
			},
			status: dirtyStatus,
			want:   branchSafety{Merged: true, Pushed: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			m.StatusOverride = tc.status
			for k, v := range tc.responses {
				m.Runner.Responses[k] = v
			}
			for k, e := range tc.errs {
				m.Runner.Errors[k] = e
			}

			got := inspectBranchSafety(m, dir, branch, def)
			if got != tc.want {
				t.Fatalf("inspectBranchSafety = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestInspectBranchSafety_DefaultBranchSelf documents that the probe
// reports Merged=false when called for the default branch itself —
// remove.go has a separate guard preventing default-branch removal,
// so the gate never sees this case in practice.
func TestInspectBranchSafety_DefaultBranchSelf(t *testing.T) {
	m := mocks.NewMockGit()
	got := inspectBranchSafety(m, "/wt", "main", "main")
	if got.Merged {
		t.Fatalf("expected Merged=false for branch==default, got %+v", got)
	}
}
