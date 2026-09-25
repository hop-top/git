package hop_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// refusedIntact asserts a refused conversion changed nothing: the
// repository is still regular, no backup was taken, and every linked
// worktree still reaches its admin dir in .git/worktrees.
func refusedIntact(t *testing.T, repo string, linked ...string) {
	t.Helper()
	if got := strings.TrimSpace(gitOut(t, "-C", repo, "rev-parse", "--is-bare-repository")); got != "false" {
		t.Errorf("repository became bare")
	}
	for _, p := range []string{filepath.Join(repo, "hop.json"), filepath.Join(repo, "hops"), filepath.Join(filepath.Dir(repo), "backups")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists after a refusal", p)
		}
	}
	for _, wt := range linked {
		if got := absGitDir(t, wt); filepath.Dir(got) != filepath.Join(repo, ".git", "worktrees") {
			t.Errorf("%s: git dir %s moved", wt, got)
		}
	}
}

// A paused operation in a linked worktree refuses the conversion before
// the backup, as one in the main worktree does, naming the worktree and
// the commands that conclude it there.
func TestConvertBare_RefusesPausedOperationInLinkedWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	for name, pause := range map[string]func(t *testing.T, wt string){
		"rebase": func(t *testing.T, wt string) {
			writeFile(t, filepath.Join(wt, "a.txt"), "feat\n", 0o644)
			mustRun(t, "git", "-C", wt, "commit", "-q", "-am", "feat")
			_ = exec.Command("git", "-C", wt, "rebase", "main").Run() // stops on the conflict
		},
		"bisect": func(t *testing.T, wt string) {
			mustRun(t, "git", "-C", wt, "bisect", "start")
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, repo := newLinkedRepo(t)
			wt := filepath.Join(root, "ext", "wt")
			mustRun(t, "git", "-C", repo, "branch", "feat")
			writeFile(t, filepath.Join(repo, "a.txt"), "main\n", 0o644)
			mustRun(t, "git", "-C", repo, "commit", "-q", "-am", "main")
			mustRun(t, "git", "-C", repo, "worktree", "add", "-q", wt, "feat")
			pause(t, wt)

			var ipe *hop.InProgressError
			if err := hop.CheckNoOperationInProgress(afero.NewOsFs(), repo); !errors.As(err, &ipe) {
				t.Fatalf("CheckNoOperationInProgress = %v, want an InProgressError", err)
			}
			if ipe.Worktree != wt || !strings.Contains(ipe.Error(), wt) {
				t.Errorf("refusal does not name the linked worktree %s: %q", wt, ipe.Error())
			}
			hints := strings.Join(ipe.Hints("git hop init"), "\n")
			if !strings.Contains(hints, "git -C "+wt+" ") {
				t.Errorf("hints do not run in the linked worktree:\n%s", hints)
			}

			_, err := convertBare(t, repo, git.New())
			if !errors.As(err, &ipe) {
				t.Fatalf("conversion error = %v, want an InProgressError", err)
			}
			refusedIntact(t, repo, wt)
		})
	}
}

// What cannot be carried is refused before the backup, with the reason.
func TestConvertBare_RefusesUncarriableLinkedWorktrees(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	for name, tc := range map[string]struct {
		setup func(t *testing.T, root, repo string) []string
		want  string
	}{
		"git running in it": {
			setup: func(t *testing.T, root, repo string) []string {
				wt := filepath.Join(root, "ext", "wt")
				mustRun(t, "git", "-C", repo, "worktree", "add", "-q", wt, "-b", "feat")
				writeFile(t, filepath.Join(repo, ".git", "worktrees", "wt", "index.lock"), "", 0o644)
				return []string{wt}
			},
			want: "index.lock",
		},
		"two worktrees on one branch": {
			setup: func(t *testing.T, root, repo string) []string {
				wt := filepath.Join(root, "ext", "wt")
				mustRun(t, "git", "-C", repo, "worktree", "add", "-q", "-f", wt, "main")
				return []string{wt}
			},
			want: "main",
		},
		"inside .git": {
			setup: func(t *testing.T, root, repo string) []string {
				wt := filepath.Join(repo, ".git", "wt")
				mustRun(t, "git", "-C", repo, "worktree", "add", "-q", wt, "-b", "feat")
				return []string{wt}
			},
			want: ".git",
		},
		"at the conversion's scratch path": {
			setup: func(t *testing.T, root, repo string) []string {
				wt := repo + ".new"
				mustRun(t, "git", "-C", repo, "worktree", "add", "-q", wt, "-b", "feat")
				return []string{wt}
			},
			want: ".new",
		},
		"inside another linked worktree": {
			setup: func(t *testing.T, root, repo string) []string {
				outer := filepath.Join(root, "ext", "outer")
				inner := filepath.Join(outer, "inner")
				mustRun(t, "git", "-C", repo, "worktree", "add", "-q", outer, "-b", "outer")
				mustRun(t, "git", "-C", repo, "worktree", "add", "-q", inner, "-b", "inner")
				return []string{outer, inner}
			},
			want: "inside",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, repo := newLinkedRepo(t)
			linked := tc.setup(t, root, repo)
			conv := hop.NewConverter(afero.NewOsFs(), git.New())
			conv.BackupRoot = filepath.Join(root, "backups")
			result, err := conv.ConvertToBareWorktree(repo, true, false)
			if !errors.Is(err, hop.ErrLinkedWorktrees) {
				t.Fatalf("conversion error = %v, want ErrLinkedWorktrees", err)
			}
			if msg := strings.Join(result.Errors, "\n"); !strings.Contains(msg, tc.want) || !strings.Contains(msg, linked[len(linked)-1]) {
				t.Errorf("refusal %q does not name %s and %q", msg, linked[len(linked)-1], tc.want)
			}
			refusedIntact(t, repo, linked...)
		})
	}
}
