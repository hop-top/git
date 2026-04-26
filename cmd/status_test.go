package cmd

import (
	"errors"
	"testing"

	"hop.top/git/test/mocks"
)

// TestComposePsHasRunning covers the empty cases for the output of
// `docker compose ps --format json`. Compose emits either an empty JSON
// array ("[]") or an empty string when no containers exist; a naive
// len(s) > 0 check would report true for "[]" and overcount.
func TestComposePsHasRunning(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string", "", false},
		{"whitespace only", "   \n", false},
		{"empty array", "[]", false},
		{"empty array with whitespace", "  [] \n", false},
		{"array with one entry", `[{"Name":"svc"}]`, true},
		{"array with multiple entries", `[{"Name":"a"},{"Name":"b"}]`, true},
		{"json lines single entry", `{"Name":"svc"}`, true},
		{"json lines multiple entries", `{"Name":"a"}
{"Name":"b"}`, true},
		{"json lines blank lines only", "\n\n\n", false},
		{"malformed non-empty (defensive true)", `garbage`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composePsHasRunning(tc.in)
			if got != tc.want {
				t.Fatalf("composePsHasRunning(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestGetBranchSyncStatus covers the branch position categories
// surfaced by `git hop status`: default, synced, merged, ahead,
// behind, diverged, and the unknown fallback. The mock returns
// rev-list --left-right --count output of the form "<ahead>\t<behind>".
func TestGetBranchSyncStatus(t *testing.T) {
	const (
		dir     = "/tmp/wt"
		def     = "main"
		branch  = "feature"
		revKey  = dir + ":git rev-list --left-right --count " + branch + "..." + def
	)

	cases := []struct {
		name          string
		branch        string
		defaultBranch string
		response      string
		err           error
		want          string
	}{
		{"default branch itself", "main", "main", "", nil, "default"},
		{"empty default falls back", "feature", "", "", nil, "default"},
		{"synced — same head", branch, def, "0\t0", nil, "synced"},
		{"merged — behind only", branch, def, "0\t3", nil, "merged (3 behind)"},
		{"ahead only", branch, def, "5\t0", nil, "5 ahead"},
		{"diverged", branch, def, "2\t4", nil, "diverged (2 ahead, 4 behind)"},
		{"git error → unknown", branch, def, "", errors.New("boom"), "unknown"},
		{"malformed output → unknown", branch, def, "garbage", nil, "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			if tc.err != nil {
				m.Runner.Errors[revKey] = tc.err
			} else {
				m.Runner.Responses[revKey] = tc.response
			}

			got := getBranchSyncStatus(m, dir, tc.branch, tc.defaultBranch)
			if got != tc.want {
				t.Fatalf("getBranchSyncStatus(%q, %q) = %q, want %q",
					tc.branch, tc.defaultBranch, got, tc.want)
			}
		})
	}
}
