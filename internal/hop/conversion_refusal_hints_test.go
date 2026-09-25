package hop_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
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
	} {
		if len(hints) == 0 || hints[len(hints)-1] != want {
			t.Errorf("%s: hints = %q, want last %q", name, hints, want)
		}
	}
}

// The linked-worktrees refusal is recognisable, so init can offer the
// --regular conversion that gets past it.
func TestConvert_LinkedWorktreesRefusalIsTyped(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	mustRun(t, "git", "-C", repo, "commit", "-q", "--allow-empty", "-m", "one")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", "-b", "feat", repo+"-feat")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.BackupRoot = filepath.Join(t.TempDir(), "backups")
	_, err := conv.ConvertToBareWorktree(repo, true, true)
	if !errors.Is(err, hop.ErrLinkedWorktrees) {
		t.Fatalf("conversion error = %v, want ErrLinkedWorktrees", err)
	}
	if err.Error() != "linked worktrees present" {
		t.Errorf("error text = %q, want it unchanged", err.Error())
	}
}
