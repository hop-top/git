package hop_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// detachedRepo is a repository with two commits and HEAD detached at the
// second: no branch is checked out.
func detachedRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	mustRun(t, "git", "-C", repo, "commit", "-q", "--allow-empty", "-m", "one")
	mustRun(t, "git", "-C", repo, "commit", "-q", "--allow-empty", "-m", "two")
	mustRun(t, "git", "-C", repo, "checkout", "-q", "--detach")
	return repo
}

// Both layouts name something after the current branch (the bare
// layout's worktree, the regular layout's hop.json entry), so a detached
// HEAD is refused before the backup and before anything moves.
func TestConvert_RefusesDetachedHead(t *testing.T) {
	for _, useBare := range []bool{true, false} {
		name := "regular"
		if useBare {
			name = "bare"
		}
		t.Run(name, func(t *testing.T) {
			repo := detachedRepo(t)
			conv := hop.NewConverter(afero.NewOsFs(), git.New())
			conv.BackupRoot = filepath.Join(t.TempDir(), "backups")

			_, err := conv.ConvertToBareWorktree(repo, useBare, true)

			var dhe *hop.DetachedHeadError
			if !errors.As(err, &dhe) {
				t.Fatalf("conversion error = %v, want a DetachedHeadError", err)
			}
			if strings.Contains(err.Error(), "%!") {
				t.Errorf("garbled error: %q", err.Error())
			}
			if hints := strings.Join(dhe.Hints(), "\n"); !strings.Contains(hints, "git switch") {
				t.Errorf("hints %q do not say how to check out a branch", hints)
			}
			if _, err := os.Stat(conv.BackupRoot); !os.IsNotExist(err) {
				t.Errorf("a backup was taken before the refusal (stat: %v)", err)
			}
			for _, p := range []string{filepath.Join(repo, "hop.json"), filepath.Join(repo, "hops"),
				filepath.Join(repo, "worktrees"), repo + ".new"} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s exists after the refusal", p)
				}
			}
		})
	}
}

// A branch checked out passes, and is what CurrentBranchForConversion
// returns.
func TestCurrentBranchForConversion(t *testing.T) {
	repo := detachedRepo(t)
	if _, err := hop.CurrentBranchForConversion(git.New(), repo); err == nil {
		t.Fatal("detached HEAD accepted")
	}
	mustRun(t, "git", "-C", repo, "checkout", "-q", "main")
	branch, err := hop.CurrentBranchForConversion(git.New(), repo)
	if err != nil || branch != "main" {
		t.Fatalf("CurrentBranchForConversion = %q, %v; want main, nil", branch, err)
	}
}
