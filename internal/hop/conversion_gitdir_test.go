package hop_test

import (
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

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func lineCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// TestConvertBare_CarriesGitDirState: user state kept in the old .git
// directory, beside its config, is still in effect after a bare
// conversion: ignore and attribute rules, user hooks, rerere records,
// notes, stashes and reflogs.
func TestConvertBare_CarriesGitDirState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := filepath.Join(t.TempDir(), "proj")
	gd := filepath.Join(repo, ".git")
	mustRun(t, "git", "init", "-b", "main", repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "one\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "a.txt")
	mustRun(t, "git", "-C", repo, "commit", "-m", "one")
	writeFile(t, filepath.Join(repo, "a.txt"), "two\n", 0o644)
	mustRun(t, "git", "-C", repo, "commit", "-am", "two")
	mustRun(t, "git", "-C", repo, "notes", "add", "-m", "a note", "HEAD")
	for _, v := range []string{"s1\n", "s2\n"} {
		writeFile(t, filepath.Join(repo, "a.txt"), v, 0o644)
		mustRun(t, "git", "-C", repo, "stash")
	}

	writeFile(t, filepath.Join(gd, "info", "exclude"), "secret.txt\n", 0o644)
	writeFile(t, filepath.Join(repo, "secret.txt"), "untracked and ignored\n", 0o644)
	writeFile(t, filepath.Join(gd, "info", "attributes"), "*.txt eol=crlf\n", 0o644)
	writeFile(t, filepath.Join(gd, "info", "sparse-checkout"), "/a.txt\n", 0o644)
	writeFile(t, filepath.Join(gd, "hooks", "pre-commit"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(gd, "description"), "my project\n", 0o644)
	writeFile(t, filepath.Join(gd, "rr-cache", "0123abcd", "postimage"), "resolved\n", 0o644)
	writeFile(t, filepath.Join(gd, "config.worktree"), "[user]\n\tname = per-worktree\n", 0o644)
	writeFile(t, filepath.Join(gd, "odd-tool-state", "data"), "x\n", 0o644)

	branchReflog := gitOut(t, "-C", repo, "reflog", "show", "--format=%H %gs", "main")
	headReflog := gitOut(t, "-C", repo, "reflog", "show", "--format=%H %gs", "HEAD")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	result, err := conv.ConvertToBareWorktree(repo, true, false)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	wt := filepath.Join(repo, "hops", "main")

	t.Run("info/exclude keeps ignored files ignored", func(t *testing.T) {
		if st := gitOut(t, "-C", wt, "status", "--porcelain"); st != "" {
			t.Errorf("status not clean after conversion:\n%s", st)
		}
		if err := exec.Command("git", "-C", wt, "check-ignore", "-q", "secret.txt").Run(); err != nil {
			t.Errorf("secret.txt is not ignored after conversion: %v", err)
		}
	})
	t.Run("info/attributes still applies", func(t *testing.T) {
		if got := gitOut(t, "-C", wt, "check-attr", "eol", "a.txt"); !strings.Contains(got, "eol: crlf") {
			t.Errorf("check-attr eol a.txt = %q, want eol: crlf", got)
		}
	})
	t.Run("info/sparse-checkout goes to the worktree's own git dir", func(t *testing.T) {
		p := strings.TrimSpace(gitOut(t, "-C", wt, "rev-parse", "--path-format=absolute", "--git-path", "info/sparse-checkout"))
		if b, err := os.ReadFile(p); err != nil || string(b) != "/a.txt\n" {
			t.Errorf("%s = %q, %v; want the original patterns", p, b, err)
		}
	})
	t.Run("user hooks carried, executable", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(repo, "hooks", "pre-commit"))
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Errorf("hub hooks/pre-commit: %v, mode %v", err, info)
		}
	})
	t.Run("description carried", func(t *testing.T) {
		if b, _ := os.ReadFile(filepath.Join(repo, "description")); string(b) != "my project\n" {
			t.Errorf("description = %q", b)
		}
	})
	t.Run("rerere records carried", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(repo, "rr-cache", "0123abcd", "postimage")); err != nil {
			t.Errorf("rr-cache entry missing: %v", err)
		}
	})
	t.Run("notes carried", func(t *testing.T) {
		if got := strings.TrimSpace(gitOut(t, "-C", wt, "notes", "show", "HEAD")); got != "a note" {
			t.Errorf("notes show HEAD = %q", got)
		}
	})
	t.Run("every stash carried", func(t *testing.T) {
		if n := lineCount(gitOut(t, "-C", wt, "stash", "list")); n != 2 {
			t.Errorf("stash list has %d entries, want 2", n)
		}
	})
	t.Run("reflogs carried", func(t *testing.T) {
		if got := gitOut(t, "-C", wt, "reflog", "show", "--format=%H %gs", "main"); got != branchReflog {
			t.Errorf("main reflog\n got:\n%s\nwant:\n%s", got, branchReflog)
		}
		if got := gitOut(t, "-C", wt, "reflog", "show", "--format=%H %gs", "HEAD"); !strings.HasSuffix(got, headReflog) {
			t.Errorf("worktree HEAD reflog lost the old history\n got:\n%s\nwant suffix:\n%s", got, headReflog)
		}
	})
	t.Run("what is left behind is named", func(t *testing.T) {
		joined := strings.Join(result.Warnings, "\n")
		for _, want := range []string{".git/config.worktree", ".git/odd-tool-state"} {
			if !strings.Contains(joined, want) {
				t.Errorf("no warning names %s; warnings:\n%s", want, joined)
			}
		}
		for _, quiet := range []string{".git/COMMIT_EDITMSG", ".git/index", ".git/objects"} {
			if strings.Contains(joined, quiet) {
				t.Errorf("warning names %s, which needs no carrying; warnings:\n%s", quiet, joined)
			}
		}
	})
}
