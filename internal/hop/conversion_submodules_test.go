package hop_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// gitFile runs git with local file:// submodule URLs allowed.
func gitFile(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "protocol.file.allow=always"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func commitRepo(t *testing.T, path string, msgs ...string) {
	t.Helper()
	mustRun(t, "git", "init", "-b", "main", path)
	for _, m := range msgs {
		mustRun(t, "git", "-C", path, "commit", "--allow-empty", "-m", m)
	}
}

// TestConvertBare_KeepsSubmodules: a checked-out submodule, and one
// nested inside it, still work in the default worktree after a bare
// conversion: status, log, and update all find their repositories.
func TestConvertBare_KeepsSubmodules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	sub := filepath.Join(root, "sub")
	repo := filepath.Join(root, "proj")
	commitRepo(t, nested, "n1")
	commitRepo(t, sub, "s1", "s2")
	gitFile(t, "-C", sub, "submodule", "add", "-q", "file://"+nested, "nested")
	mustRun(t, "git", "-C", sub, "commit", "-m", "add nested")
	commitRepo(t, repo, "p1")
	gitFile(t, "-C", repo, "submodule", "add", "-q", "file://"+sub, "lib")
	mustRun(t, "git", "-C", repo, "commit", "-m", "add lib")
	gitFile(t, "-C", repo, "submodule", "update", "--init", "--recursive", "-q")
	libLog := gitOut(t, "-C", filepath.Join(repo, "lib"), "log", "--format=%H")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	result, err := conv.ConvertToBareWorktree(repo, true, false)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	wt := filepath.Join(repo, "hops", "main")
	lib := filepath.Join(wt, "lib")

	t.Run("status clean", func(t *testing.T) {
		if st := gitFile(t, "-C", wt, "status", "--porcelain"); st != "" {
			t.Errorf("status not clean:\n%s", st)
		}
	})
	t.Run("submodule status finds every module", func(t *testing.T) {
		for _, line := range strings.Split(strings.TrimRight(gitFile(t, "-C", wt, "submodule", "status", "--recursive"), "\n"), "\n") {
			if line == "" || line[0] != ' ' {
				t.Errorf("submodule not in sync: %q", line)
			}
		}
	})
	t.Run("submodule history intact", func(t *testing.T) {
		if got := gitFile(t, "-C", lib, "log", "--format=%H"); got != libLog {
			t.Errorf("lib log\n got:\n%s\nwant:\n%s", got, libLog)
		}
		gitFile(t, "-C", filepath.Join(lib, "nested"), "log", "--oneline")
	})
	t.Run("module git dirs live in the default worktree's git dir", func(t *testing.T) {
		wtGit := strings.TrimSpace(gitFile(t, "-C", wt, "rev-parse", "--absolute-git-dir"))
		for rel, want := range map[string]string{
			"lib":        filepath.Join(wtGit, "modules", "lib"),
			"lib/nested": filepath.Join(wtGit, "modules", "lib", "modules", "nested"),
		} {
			got := strings.TrimSpace(gitFile(t, "-C", filepath.Join(wt, rel), "rev-parse", "--absolute-git-dir"))
			if got != want {
				t.Errorf("%s git dir = %s, want %s", rel, got, want)
			}
			top := strings.TrimSpace(gitFile(t, "-C", filepath.Join(wt, rel), "rev-parse", "--show-toplevel"))
			if wantTop, _ := filepath.EvalSymlinks(filepath.Join(wt, rel)); top != wantTop {
				t.Errorf("%s toplevel = %s, want %s", rel, top, wantTop)
			}
		}
	})
	t.Run("submodule update works", func(t *testing.T) {
		gitFile(t, "-C", lib, "checkout", "-q", "HEAD~1")
		gitFile(t, "-C", wt, "submodule", "update", "--recursive", "-q")
		if got := strings.TrimSpace(gitFile(t, "-C", lib, "rev-parse", "HEAD")); got != strings.SplitN(libLog, "\n", 2)[0] {
			t.Errorf("submodule update left lib at %s", got)
		}
	})
	t.Run("no submodule warning", func(t *testing.T) {
		for _, w := range result.Warnings {
			if strings.Contains(w, "modules") {
				t.Errorf("unexpected warning: %s", w)
			}
		}
	})
}
