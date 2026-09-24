package hop_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// stagedRepo is a repository with every kind of uncommitted change: a
// file both staged and further edited (partially staged), a staged add,
// delete, rename, binary change and mode change, an intent-to-add file,
// an unstaged deletion, and an untracked file.
func stagedRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	writeFile(t, filepath.Join(repo, "mod.txt"), "one\ntwo\n", 0o644)
	writeFile(t, filepath.Join(repo, "del.txt"), "deleted\n", 0o644)
	writeFile(t, filepath.Join(repo, "dir", "ren.txt"), "renamed\nas is\n", 0o644)
	writeFile(t, filepath.Join(repo, "mode.sh"), "#!/bin/sh\n", 0o644)
	writeFile(t, filepath.Join(repo, "bin.dat"), "\x00\x01\x02binary", 0o644)
	writeFile(t, filepath.Join(repo, "gone.txt"), "removed, not staged\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", ".")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "base")

	writeFile(t, filepath.Join(repo, "mod.txt"), "one\nTWO\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "mod.txt")
	writeFile(t, filepath.Join(repo, "mod.txt"), "one\nTWO\nthree\n", 0o644)
	mustRun(t, "git", "-C", repo, "rm", "-q", "del.txt")
	mustRun(t, "git", "-C", repo, "mv", "dir/ren.txt", "renamed.txt")
	if err := os.Chmod(filepath.Join(repo, "mode.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "-C", repo, "add", "mode.sh")
	writeFile(t, filepath.Join(repo, "bin.dat"), "\x00\x03\x04changed", 0o644)
	mustRun(t, "git", "-C", repo, "add", "bin.dat")
	writeFile(t, filepath.Join(repo, "added.txt"), "new\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "added.txt")
	writeFile(t, filepath.Join(repo, "ita.txt"), "intent\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "-N", "ita.txt")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "untracked.txt"), "untracked\n", 0o644)
	return repo
}

// indexView is what a user sees of the index and the working tree.
type indexView struct{ status, cached, unstaged string }

func viewOf(t *testing.T, dir string) indexView {
	t.Helper()
	return indexView{
		status:   gitOut(t, "-C", dir, "status", "--porcelain=v2", "--untracked-files=all"),
		cached:   gitOut(t, "-C", dir, "diff", "--no-ext-diff", "--cached", "--binary"),
		unstaged: gitOut(t, "-C", dir, "diff", "--no-ext-diff", "--binary"),
	}
}

func compareViews(t *testing.T, got, want indexView) {
	t.Helper()
	// porcelain=v2 prints each entry's modes and object names, so equal
	// output means equal index entries, not just equal names.
	if got.status != want.status {
		t.Errorf("git status differs\ngot:\n%s\nwant:\n%s", got.status, want.status)
	}
	if got.cached != want.cached {
		t.Errorf("git diff --cached differs\ngot:\n%s\nwant:\n%s", got.cached, want.cached)
	}
	if got.unstaged != want.unstaged {
		t.Errorf("git diff differs\ngot:\n%s\nwant:\n%s", got.unstaged, want.unstaged)
	}
}

// TestConvertBare_ForceCarriesStagedState: init --force carries the
// index, so the new worktree shows the same staged/unstaged split,
// deletions included, as the repository did before.
func TestConvertBare_ForceCarriesStagedState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := stagedRepo(t)
	before := viewOf(t, repo)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.Force = true
	result, err := conv.ConvertToBareWorktree(repo, true, true)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "stag") {
			t.Errorf("unexpected warning: %s", w)
		}
	}
	compareViews(t, viewOf(t, filepath.Join(repo, "hops", "main")), before)
}

// failApply is real git whose `git apply` fails, standing in for a
// staged change the new index cannot take.
type failApply struct{ git.GitInterface }

func (f failApply) Run(cmd string, args ...string) (string, error) {
	for _, a := range args {
		if a == "apply" {
			return "", errors.New("apply refused")
		}
	}
	return f.GitInterface.Run(cmd, args...)
}

// When the staged changes cannot be put back in the index, the
// conversion still succeeds with every change in the working tree,
// unstaged, and says so: content is never lost.
func TestConvertBare_ForceStagedRestoreFailsSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := stagedRepo(t)
	contents := map[string]string{}
	for _, f := range []string{"mod.txt", "renamed.txt", "mode.sh", "bin.dat", "added.txt", "ita.txt", "untracked.txt"} {
		b, err := os.ReadFile(filepath.Join(repo, f))
		if err != nil {
			t.Fatal(err)
		}
		contents[f] = string(b)
	}

	conv := hop.NewConverter(afero.NewOsFs(), failApply{git.New()})
	conv.Force = true
	result, err := conv.ConvertToBareWorktree(repo, true, true)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "left unstaged") {
		t.Errorf("no warning that the staged changes were left unstaged; warnings: %v", result.Warnings)
	}

	wt := filepath.Join(repo, "hops", "main")
	for f, want := range contents {
		got, err := os.ReadFile(filepath.Join(wt, f))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q (%v), want %q", f, got, err, want)
		}
	}
	if st, _ := os.Stat(filepath.Join(wt, "mode.sh")); st == nil || st.Mode()&0o111 == 0 {
		t.Errorf("mode.sh lost its exec bit")
	}
	if cached := gitOut(t, "-C", wt, "diff", "--cached", "--name-only"); cached != "" {
		t.Errorf("index not left at HEAD after a failed restore:\n%s", cached)
	}
}

// User config that reshapes diff output or makes apply strict does not
// get in the way: the patch is read back as it was written.
func TestConvertBare_ForceCarriesStagedState_UserConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := stagedRepo(t)
	for _, kv := range [][2]string{
		{"diff.noprefix", "true"}, {"color.diff", "always"}, {"diff.context", "0"},
		{"diff.external", "false"}, {"diff.renames", "copies"},
	} {
		mustRun(t, "git", "-C", repo, "config", kv[0], kv[1])
	}
	// Global, so it reaches the new worktree too, and a staged line it
	// would reject.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "apply.whitespace")
	t.Setenv("GIT_CONFIG_VALUE_0", "error")
	writeFile(t, filepath.Join(repo, "ws.txt"), "trailing space \n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "ws.txt")
	before := viewOf(t, repo)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.Force = true
	result, err := conv.ConvertToBareWorktree(repo, true, true)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	compareViews(t, viewOf(t, filepath.Join(repo, "hops", "main")), before)
}

// An unmerged path (a conflicted stash pop) cannot keep its stages: its
// content, markers included, comes back unstaged, with a warning, and
// the rest of the index is still restored.
func TestConvertBare_ForceUnmergedPathWarns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	writeFile(t, filepath.Join(repo, "a.txt"), "base\n", 0o644)
	writeFile(t, filepath.Join(repo, "s.txt"), "s\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", ".")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "base")
	writeFile(t, filepath.Join(repo, "a.txt"), "stashed\n", 0o644)
	mustRun(t, "git", "-C", repo, "stash", "-q")
	writeFile(t, filepath.Join(repo, "a.txt"), "committed\n", 0o644)
	mustRun(t, "git", "-C", repo, "commit", "-q", "-am", "other")
	writeFile(t, filepath.Join(repo, "s.txt"), "staged\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", "s.txt")
	gitMayFail(t, repo, nil, "stash", "pop")
	if st := gitOut(t, "-C", repo, "status", "--porcelain"); !strings.Contains(st, "UU a.txt") {
		t.Fatalf("setup left no unmerged a.txt:\n%s", st)
	}
	conflicted, err := os.ReadFile(filepath.Join(repo, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.Force = true
	result, err := conv.ConvertToBareWorktree(repo, true, true)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	if w := strings.Join(result.Warnings, "\n"); !strings.Contains(w, "a.txt: unmerged") {
		t.Errorf("no warning about the unmerged a.txt; warnings: %v", result.Warnings)
	}
	wt := filepath.Join(repo, "hops", "main")
	if got, _ := os.ReadFile(filepath.Join(wt, "a.txt")); string(got) != string(conflicted) {
		t.Errorf("a.txt = %q, want the conflicted content %q", got, conflicted)
	}
	if st := gitOut(t, "-C", wt, "status", "--porcelain"); st != "MM a.txt\nM  s.txt\n" && st != " M a.txt\nM  s.txt\n" {
		t.Errorf("status after conversion:\n%s\nwant a.txt unstaged and s.txt staged", st)
	}
}
