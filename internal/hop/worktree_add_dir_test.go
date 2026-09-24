package hop

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/test/mocks"
)

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// worktreeAddDir picks the repository, never a linked worktree, as the
// place `git worktree add` runs; a --regular hub keeps using its root.
func TestWorktreeAddDir(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fs := afero.NewOsFs()
	g := git.New()

	seed := filepath.Join(dir, "seed")
	runGit(t, "init", "-q", "-b", "main", seed)
	runGit(t, "-C", seed, "commit", "-q", "--allow-empty", "-m", "one")
	runGit(t, "-C", seed, "commit", "-q", "--allow-empty", "-m", "two")

	hub := filepath.Join(dir, "hub")
	runGit(t, "clone", "-q", "--bare", seed, hub)
	runGit(t, "-C", hub, "worktree", "add", "-q", "hops/main", "main")

	reg := filepath.Join(dir, "reg")
	runGit(t, "clone", "-q", seed, reg)
	regFeat := filepath.Join(dir, "reg-feat")
	runGit(t, "-C", reg, "worktree", "add", "-q", "-b", "feat", regFeat)

	dotBare := filepath.Join(dir, "dotbare")
	runGit(t, "clone", "-q", "--bare", seed, filepath.Join(dotBare, ".git"))
	runGit(t, "-C", dotBare, "worktree", "add", "-q", "hops/main", "main")

	for _, tc := range []struct{ name, base, want string }{
		{"worktree of a bare hub", filepath.Join(hub, "hops", "main"), hub},
		{"bare hub itself", hub, hub},
		{"--regular hub root is unchanged", reg, reg},
		{"linked worktree of a regular repo", regFeat, reg},
		{"worktree of a bare repo kept in .git", filepath.Join(dotBare, "hops", "main"), filepath.Join(dotBare, ".git")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := worktreeAddDir(fs, g, tc.base); got != tc.want {
				t.Fatalf("worktreeAddDir(%s) = %s, want %s", tc.base, got, tc.want)
			}
		})
	}

	t.Run("no answer from git keeps base", func(t *testing.T) {
		if got := worktreeAddDir(afero.NewMemMapFs(), mocks.NewMockGit(), "/x/hops/main"); got != "/x/hops/main" {
			t.Fatalf("got %s, want base", got)
		}
	})

	t.Run("pinStartPoint", func(t *testing.T) {
		main := filepath.Join(hub, "hops", "main")
		runGit(t, "-C", main, "checkout", "-q", "--detach", "HEAD~1")
		old := runGit(t, "-C", main, "rev-parse", "HEAD")
		for ref, want := range map[string]string{
			"HEAD":            old,    // per worktree: pinned
			"main":            "main", // shared: passed on for tracking rules
			"refs/heads/main": "refs/heads/main",
			"no-such-ref":     "no-such-ref",
			"":                "",
		} {
			if got := pinStartPoint(g, main, hub, ref); got != want {
				t.Errorf("pinStartPoint(%q) = %q, want %q", ref, got, want)
			}
		}
	})
}
