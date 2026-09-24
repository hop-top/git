package hop

import (
	"strings"
	"testing"
)

// TestGitDirWarning: every .git entry is carried, left behind quietly,
// or named in a warning; never dropped without a decision.
func TestGitDirWarning(t *testing.T) {
	for _, tc := range []struct {
		name string
		warn string // substring of the warning; "" means none
	}{
		{"info", ""},
		{"hooks", ""},
		{"logs", ""},
		{"packed-refs", ""},
		{"objects", ""},
		{"index", ""},
		{"sharedindex.0123abcd", ""},
		{"BISECT_LOG", ""},
		{"ORIG_HEAD", ""},
		{"BISECT_START", "bisect was in progress"},
		{"rebase-merge", "rebase was in progress"},
		{"MERGE_HEAD", "merge was in progress"},
		{"config.worktree", "per-worktree config"},
		{"some-tool", ".git/some-tool: not carried over"},
	} {
		got := gitDirWarning(tc.name)
		if tc.warn == "" && got != "" {
			t.Errorf("%s: unexpected warning %q", tc.name, got)
		}
		if tc.warn != "" && !strings.Contains(got, tc.warn) {
			t.Errorf("%s: warning %q, want it to contain %q", tc.name, got, tc.warn)
		}
	}
}

// TestGitDirRulesDisjoint: an entry has exactly one fate.
func TestGitDirRulesDisjoint(t *testing.T) {
	seen := map[string]string{}
	for label, m := range map[string]map[string]string{
		"carried": gitDirCarried, "left behind": gitDirLeftBehind, "warned": gitDirWarned,
	} {
		for name := range m {
			if prev, ok := seen[name]; ok {
				t.Errorf(".git/%s is both %s and %s", name, prev, label)
			}
			seen[name] = label
		}
	}
}
