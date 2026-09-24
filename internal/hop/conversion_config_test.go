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

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// localConfigLines is `git config --local --list` as lines, dropping the
// keys a conversion hands to the hub's own layout.
func localConfigLines(t *testing.T, repo string, drop ...string) []string {
	t.Helper()
	var lines []string
next:
	for _, l := range strings.Split(strings.TrimSpace(gitOut(t, "-C", repo, "config", "--local", "--list")), "\n") {
		for _, d := range drop {
			if strings.HasPrefix(l, d) {
				continue next
			}
		}
		lines = append(lines, l)
	}
	return lines
}

// TestConvertBare_CarriesLocalConfig: every local config entry that is
// not excluded is in the hub afterwards, value for value and in the same
// order; excluded ones follow the hub's layout; nothing is doubled.
func TestConvertBare_CarriesLocalConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "commit", "--allow-empty", "-m", "init")
	for _, kv := range [][]string{
		{"user.name", "Local Name"},
		{"user.email", "local@example.test"},
		{"url.https://mirror.example.test/.insteadOf", "https://git.example.test/"},
		{"core.hooksPath", ".githooks"},
		{"alias.st", "status -sb"},
		{"hop.backup.maxBackups", "7"},
		{"include.path", "/nonexistent/shared.gitconfig"},
		{"core.worktree", repo},
		{"extensions.worktreeConfig", "true"},
		{"core.filemode", "false"}, // an override of the clone's probe
	} {
		mustRun(t, "git", "-C", repo, "config", kv[0], kv[1])
	}
	for _, v := range []string{"b", "a", "c"} {
		mustRun(t, "git", "-C", repo, "config", "--add", "hop.multi", v)
	}
	// a key with no "=", which git reads as true
	f, err := os.OpenFile(filepath.Join(repo, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("[hop \"flag\"]\n\timplicit\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	excluded := []string{"core.bare=", "core.worktree=", "core.repositoryformatversion=", "extensions."}
	want := localConfigLines(t, repo, excluded...)
	want[len(want)-1] = "hop.flag.implicit=true" // --list shows the bare key without "="

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	result, err := conv.ConvertToBareWorktree(repo, true, false)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}

	got := localConfigLines(t, repo, excluded...)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("hub config differs from the original's carried keys\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if bare := strings.TrimSpace(gitOut(t, "-C", repo, "config", "--local", "--get-all", "core.bare")); bare != "true" {
		t.Errorf("core.bare = %q, want the hub's own single true", bare)
	}
	for _, key := range []string{"core.worktree", "extensions.worktreeConfig"} {
		if out, err := exec.Command("git", "-C", repo, "config", "--local", "--get-all", key).Output(); err == nil {
			t.Errorf("%s carried into the hub: %q", key, out)
		}
	}
	if ff := strings.TrimSpace(gitOut(t, "-C", repo, "config", "--local", "--get-all", "core.filemode")); ff != "false" {
		t.Errorf("core.filemode = %q, want the original's single override false", ff)
	}
	if b := strings.TrimSpace(gitOut(t, "-C", repo, "config", "--type=bool", "hop.flag.implicit")); b != "true" {
		t.Errorf("hop.flag.implicit = %q, want true", b)
	}
}

// TestConvertRegular_KeepsLocalConfig: a regular conversion leaves .git
// where it is, so its config is untouched, excluded keys included.
func TestConvertRegular_KeepsLocalConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "commit", "--allow-empty", "-m", "init")
	mustRun(t, "git", "-C", repo, "config", "user.email", "local@example.test")
	mustRun(t, "git", "-C", repo, "config", "include.path", "../shared.gitconfig")
	mustRun(t, "git", "-C", repo, "config", "extensions.worktreeConfig", "true")
	want := gitOut(t, "-C", repo, "config", "--local", "--list")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	if result, err := conv.ConvertToBareWorktree(repo, false, false); err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	if got := gitOut(t, "-C", repo, "config", "--local", "--list"); got != want {
		t.Errorf("regular conversion changed the local config\n got:\n%s\nwant:\n%s", got, want)
	}
}
