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

// gitMayFail runs git in repo and ignores its exit status: the commands
// that leave an operation in progress stop with a conflict on purpose.
func gitMayFail(t *testing.T, repo string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	_ = cmd.Run()
}

// conflictRepo is a repository on main whose branch "other" conflicts
// with it on a.txt, and adds b.txt on top of that.
func conflictRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	writeFile(t, filepath.Join(repo, "a.txt"), "base\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "a.txt")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "base")
	mustRun(t, "git", "-C", repo, "checkout", "-q", "-b", "other")
	writeFile(t, filepath.Join(repo, "a.txt"), "other\n", 0o644)
	mustRun(t, "git", "-C", repo, "commit", "-q", "-am", "other a")
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "b.txt")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "other b")
	mustRun(t, "git", "-C", repo, "checkout", "-q", "main")
	writeFile(t, filepath.Join(repo, "a.txt"), "main\n", 0o644)
	mustRun(t, "git", "-C", repo, "commit", "-q", "-am", "main a")
	writeFile(t, filepath.Join(repo, "c.txt"), "c\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "c.txt")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "main c")
	return repo
}

// inProgressCases leaves each kind of operation stopped midway, some
// with a clean working tree (which the clean check alone lets through).
var inProgressCases = []struct {
	name   string
	marker string // .git entry the operation leaves
	op     string // GitOperation.Name reported
	abort  string // command the hint must name
	setup  func(t *testing.T, repo string)
}{
	{"merge with a clean tree", "MERGE_HEAD", "merge", "git merge --abort", func(t *testing.T, repo string) {
		mustRun(t, "git", "-C", repo, "merge", "-q", "--no-commit", "-s", "ours", "other")
	}},
	{"merge with a conflict", "MERGE_HEAD", "merge", "git merge --abort", func(t *testing.T, repo string) {
		gitMayFail(t, repo, nil, "merge", "other")
	}},
	{"rebase at an edit stop", "rebase-merge", "rebase", "git rebase --abort", func(t *testing.T, repo string) {
		gitMayFail(t, repo, []string{"GIT_SEQUENCE_EDITOR=sed -i.bak 1s/^pick/edit/"}, "rebase", "-q", "-i", "HEAD~1")
	}},
	{"rebase on the apply backend", "rebase-apply", "rebase", "git rebase --abort", func(t *testing.T, repo string) {
		gitMayFail(t, repo, nil, "rebase", "--apply", "other")
	}},
	{"am", "rebase-apply", "am", "git am --abort", func(t *testing.T, repo string) {
		patch := filepath.Join(t.TempDir(), "p.patch")
		out, err := exec.Command("git", "-C", repo, "format-patch", "--stdout", "main..other").Output()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, patch, string(out), 0o644)
		gitMayFail(t, repo, nil, "am", patch)
	}},
	{"cherry-pick", "CHERRY_PICK_HEAD", "cherry-pick", "git cherry-pick --abort", func(t *testing.T, repo string) {
		gitMayFail(t, repo, nil, "cherry-pick", "other~1")
	}},
	{"cherry-pick sequence with a clean tree", "sequencer", "cherry-pick", "git cherry-pick --abort", func(t *testing.T, repo string) {
		// The conflict is committed, which ends CHERRY_PICK_HEAD but
		// leaves the sequence waiting for --continue.
		gitMayFail(t, repo, nil, "cherry-pick", "other~1", "other")
		writeFile(t, filepath.Join(repo, "a.txt"), "resolved\n", 0o644)
		mustRun(t, "git", "-C", repo, "add", "a.txt")
		mustRun(t, "git", "-C", repo, "commit", "-q", "--no-edit")
	}},
	{"revert", "REVERT_HEAD", "revert", "git revert --abort", func(t *testing.T, repo string) {
		// A later change to a.txt makes reverting "main a" conflict.
		writeFile(t, filepath.Join(repo, "a.txt"), "later\n", 0o644)
		mustRun(t, "git", "-C", repo, "commit", "-q", "-am", "later a")
		gitMayFail(t, repo, nil, "revert", "--no-edit", "HEAD~2")
	}},
	{"bisect", "BISECT_START", "bisect", "git bisect reset", func(t *testing.T, repo string) {
		mustRun(t, "git", "-C", repo, "bisect", "start")
	}},
}

// TestConvertBare_RefusesOperationInProgress: a bare conversion with a
// git operation stopped midway is refused before anything is backed up
// or moved, and --force does not override it: the operation's state
// would be abandoned.
func TestConvertBare_RefusesOperationInProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	for _, tc := range inProgressCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := conflictRepo(t)
			tc.setup(t, repo)
			if _, err := os.Stat(filepath.Join(repo, ".git", tc.marker)); err != nil {
				t.Fatalf("setup did not leave .git/%s: %v", tc.marker, err)
			}

			conv := hop.NewConverter(afero.NewOsFs(), git.New())
			conv.Force = true
			conv.BackupRoot = filepath.Join(t.TempDir(), "backups")
			_, err := conv.ConvertToBareWorktree(repo, true, true)

			var ipe *hop.InProgressError
			if !errors.As(err, &ipe) {
				t.Fatalf("conversion error = %v, want an InProgressError", err)
			}
			if len(ipe.Ops) != 1 || ipe.Ops[0].Name != tc.op {
				t.Errorf("operations = %+v, want one %q", ipe.Ops, tc.op)
			}
			if !strings.Contains(ipe.Error(), tc.op) {
				t.Errorf("error %q does not name the %s", ipe.Error(), tc.op)
			}
			if hints := strings.Join(ipe.Hints(), "\n"); !strings.Contains(hints, tc.abort) {
				t.Errorf("hints %q do not name %q", hints, tc.abort)
			}

			if _, err := os.Stat(conv.BackupRoot); !os.IsNotExist(err) {
				t.Errorf("a backup was taken before the refusal (stat: %v)", err)
			}
			if _, err := os.Stat(filepath.Join(repo, ".git", tc.marker)); err != nil {
				t.Errorf(".git/%s gone after the refusal: %v", tc.marker, err)
			}
			for _, p := range []string{filepath.Join(repo, "hop.json"), filepath.Join(repo, "hops"), repo + ".new"} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s exists after the refusal", p)
				}
			}
		})
	}
}

// The refusal is specific to a bare conversion: a regular one keeps the
// repository's own .git, so the operation survives it.
func TestConvertRegular_KeepsOperationInProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := conflictRepo(t)
	mustRun(t, "git", "-C", repo, "bisect", "start")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.BackupRoot = filepath.Join(t.TempDir(), "backups")
	if result, err := conv.ConvertToBareWorktree(repo, false, true); err != nil {
		t.Fatalf("regular conversion failed: %v (%v)", err, result.Errors)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "BISECT_START")); err != nil {
		t.Errorf("bisect state lost by a regular conversion: %v", err)
	}
}

// Two operations at once (a bisect with a merge stopped inside it) are
// both named.
func TestInProgressOperations_ReportsEach(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := conflictRepo(t)
	mustRun(t, "git", "-C", repo, "bisect", "start")
	mustRun(t, "git", "-C", repo, "merge", "-q", "--no-commit", "-s", "ours", "other")

	ops := hop.InProgressOperations(afero.NewOsFs(), filepath.Join(repo, ".git"))
	var names []string
	for _, op := range ops {
		names = append(names, op.Name)
	}
	if got := strings.Join(names, ","); got != "merge,bisect" {
		t.Errorf("operations = %s, want merge,bisect", got)
	}
	want := "a merge and a bisect are in progress; converting would abandon them"
	if got := (&hop.InProgressError{Ops: ops}).Error(); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if clean := hop.InProgressOperations(afero.NewOsFs(), filepath.Join(conflictRepo(t), ".git")); len(clean) != 0 {
		t.Errorf("operations in an idle repository = %+v, want none", clean)
	}
}
