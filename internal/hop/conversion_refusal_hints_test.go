package hop_test

import (
	"testing"

	"hop.top/git/internal/hop"
)

// A refusal's hints end by offering the retry the caller built, the
// user's own command line, rather than a bare `git hop init` that drops
// the flags the user gave.
func TestRefusalHints_EndWithCallersRetry(t *testing.T) {
	const retry = "git hop init --no-prompt --regular --dry-run"
	want := "then run " + retry + " again"
	for name, hints := range map[string][]string{
		"detached":    (&hop.DetachedHeadError{}).Hints(retry),
		"in progress": (&hop.InProgressError{Ops: []hop.GitOperation{{Name: "merge"}}}).Hints(retry),
		"in progress in a linked worktree": (&hop.InProgressError{
			Ops: []hop.GitOperation{{Name: "merge"}}, Worktree: "/x/wt"}).Hints(retry),
	} {
		if len(hints) == 0 || hints[len(hints)-1] != want {
			t.Errorf("%s: hints = %q, want last %q", name, hints, want)
		}
	}
}
