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
//
// Where core.bare lives depends on extensions.worktreeConfig: without it,
// the hub keeps the clone's core.bare=true in its shared config; with it,
// core.bare=true must sit in the hub's own config.worktree, since the
// shared config then also applies to every linked worktree.
func TestConvertBare_CarriesLocalConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	for _, worktreeConfig := range []bool{false, true} {
		name := "without extensions.worktreeConfig"
		if worktreeConfig {
			name = "with extensions.worktreeConfig"
		}
		t.Run(name, func(t *testing.T) {
			repo := filepath.Join(t.TempDir(), "proj")
			mustRun(t, "git", "init", "-b", "main", repo)
			mustRun(t, "git", "-C", repo, "commit", "--allow-empty", "-m", "init")
			kvs := [][]string{
				{"user.name", "Local Name"},
				{"user.email", "local@example.test"},
				{"url.https://mirror.example.test/.insteadOf", "https://git.example.test/"},
				{"core.hooksPath", ".githooks"},
				{"alias.st", "status -sb"},
				{"hop.backup.maxBackups", "7"},
				{"include.path", "/nonexistent/shared.gitconfig"},
				{"core.worktree", repo},
			}
			if worktreeConfig {
				kvs = append(kvs, []string{"extensions.worktreeConfig", "true"})
			}
			kvs = append(kvs, []string{"core.filemode", "false"}) // an override of the clone's probe
			for _, kv := range kvs {
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

			hubWorktreeConfig := filepath.Join(repo, "config.worktree")
			sharedBare, sharedErr := exec.Command("git", "-C", repo, "config", "--local", "--get-all", "core.bare").Output()
			if worktreeConfig {
				// Pinned deliberately: git requires core.bare=true out of
				// the shared config once the extension is on.
				if sharedErr == nil {
					t.Errorf("core.bare left in the hub's shared config: %q", sharedBare)
				}
				if bare := strings.TrimSpace(gitOut(t, "config", "--file", hubWorktreeConfig, "--get-all", "core.bare")); bare != "true" {
					t.Errorf("hub config.worktree core.bare = %q, want a single true", bare)
				}
				if ext := strings.TrimSpace(gitOut(t, "-C", repo, "config", "--local", "--type=bool", "extensions.worktreeConfig")); ext != "true" {
					t.Errorf("extensions.worktreeConfig = %q, want true", ext)
				}
			} else {
				if bare := strings.TrimSpace(string(sharedBare)); sharedErr != nil || bare != "true" {
					t.Errorf("core.bare = %q (%v), want the hub's own single true", bare, sharedErr)
				}
				if _, err := os.Stat(hubWorktreeConfig); err == nil {
					t.Errorf("hub has a config.worktree without the extension")
				}
				if out, err := exec.Command("git", "-C", repo, "config", "--local", "--get-all", "extensions.worktreeConfig").Output(); err == nil {
					t.Errorf("extensions.worktreeConfig carried into the hub: %q", out)
				}
			}
			if out, err := exec.Command("git", "-C", repo, "config", "--local", "--get-all", "core.worktree").Output(); err == nil {
				t.Errorf("core.worktree carried into the hub: %q", out)
			}
			if bare := strings.TrimSpace(gitOut(t, "-C", repo, "rev-parse", "--is-bare-repository")); bare != "true" {
				t.Errorf("hub is-bare-repository = %q, want true", bare)
			}
			if bare := strings.TrimSpace(gitOut(t, "-C", filepath.Join(repo, "hops", "main"), "rev-parse", "--is-bare-repository")); bare != "false" {
				t.Errorf("hops/main is-bare-repository = %q, want false", bare)
			}
			if ff := strings.TrimSpace(gitOut(t, "-C", repo, "config", "--local", "--get-all", "core.filemode")); ff != "false" {
				t.Errorf("core.filemode = %q, want the original's single override false", ff)
			}
			if b := strings.TrimSpace(gitOut(t, "-C", repo, "config", "--type=bool", "hop.flag.implicit")); b != "true" {
				t.Errorf("hop.flag.implicit = %q, want true", b)
			}
		})
	}
}

// TestConvertBare_CarriesWorktreeConfig: with extensions.worktreeConfig
// on, the repository's config.worktree becomes the default worktree's
// own, keys that describe the old layout excepted, and its sparse
// checkout is applied again. A worktree added afterwards gets none of it.
func TestConvertBare_CarriesWorktreeConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	for _, f := range []string{"a/x", "b/y", "top"} {
		p := filepath.Join(repo, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, "git", "-C", repo, "add", "-A")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "files")
	mustRun(t, "git", "-C", repo, "sparse-checkout", "set", "--cone", "a")
	mustRun(t, "git", "-C", repo, "config", "--worktree", "hop.test.key", "v1")
	mustRun(t, "git", "-C", repo, "config", "--worktree", "--add", "hop.test.multi", "x")
	mustRun(t, "git", "-C", repo, "config", "--worktree", "--add", "hop.test.multi", "y")
	// layout keys in the old config.worktree stay behind
	mustRun(t, "git", "-C", repo, "config", "--worktree", "core.bare", "false")
	sparseBefore := gitOut(t, "-C", repo, "sparse-checkout", "list")

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	result, err := conv.ConvertToBareWorktree(repo, true, false)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "config.worktree") {
			t.Errorf("unexpected warning: %s", w)
		}
	}

	main := filepath.Join(repo, "hops", "main")
	if got := strings.TrimSpace(gitOut(t, "-C", main, "config", "--worktree", "--get", "hop.test.key")); got != "v1" {
		t.Errorf("hops/main hop.test.key = %q, want v1", got)
	}
	if got := strings.TrimSpace(gitOut(t, "-C", main, "config", "--worktree", "--get-all", "hop.test.multi")); got != "x\ny" {
		t.Errorf("hops/main hop.test.multi = %q, want x, y in order", got)
	}
	if out, err := exec.Command("git", "-C", main, "config", "--worktree", "--get-all", "core.bare").Output(); err == nil {
		t.Errorf("old config.worktree core.bare carried: %q", out)
	}
	if got := gitOut(t, "-C", main, "sparse-checkout", "list"); got != sparseBefore {
		t.Errorf("sparse-checkout list = %q, want %q", got, sparseBefore)
	}
	if _, err := os.Stat(filepath.Join(main, "b")); err == nil {
		t.Errorf("hops/main/b present: sparse checkout not applied")
	}
	if _, err := os.Stat(filepath.Join(main, "a", "x")); err != nil {
		t.Errorf("hops/main/a/x missing: %v", err)
	}
	if st := gitOut(t, "-C", main, "status", "--porcelain"); st != "" {
		t.Errorf("hops/main not clean after conversion:\n%s", st)
	}

	feat := filepath.Join(repo, "hops", "feat")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", "-b", "feat", feat, "main")
	if bare := strings.TrimSpace(gitOut(t, "-C", feat, "rev-parse", "--is-bare-repository")); bare != "false" {
		t.Errorf("hops/feat is-bare-repository = %q, want false", bare)
	}
	if out, err := exec.Command("git", "-C", feat, "config", "--get", "hop.test.key").Output(); err == nil {
		t.Errorf("hops/feat inherited hop.test.key: %q", out)
	}
	if _, err := os.Stat(filepath.Join(feat, "b", "y")); err != nil {
		t.Errorf("hops/feat is not a full checkout: %v", err)
	}
	mustRun(t, "git", "-C", feat, "commit", "-q", "--allow-empty", "-m", "on feat")
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
