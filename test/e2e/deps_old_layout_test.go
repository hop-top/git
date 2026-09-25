package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/services"
)

// Earlier releases linked node_modules to <store>/node_modules.<hash>, the
// install itself, where Node cannot resolve the install's packages from
// one another. A worktree still linked there is reported by doctor as a
// warning, left alone by gc while linked, relinked by doctor --fix to the
// install in the current layout, and its old install is then collected.
func TestDeps_OldLayoutLink_DoctorWarnsFixRelinksGCCollects(t *testing.T) {
	t.Parallel()
	env := SetupTestEnv(t)

	// A package manager any machine has: sh writes the install.
	managers := filepath.Join(env.RootDir, ".config", "git-hop")
	if err := os.MkdirAll(managers, 0o755); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, filepath.Join(managers, "managers.json"), `{"packageManagers":[{
		"name":"fakepm","detectFiles":["fake.lock"],"lockFiles":["fake.lock"],
		"depsDir":"node_modules",
		"installCmd":["sh","-c","mkdir -p node_modules/pkg && echo new > node_modules/pkg/index.js"]}]}`)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	WriteFile(t, filepath.Join(env.SeedRepoPath, "fake.lock"), "v1\n")
	WriteFile(t, filepath.Join(env.SeedRepoPath, ".gitignore"), "node_modules\n")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature")

	pm := services.PackageManager{DepsDir: "node_modules"}
	hash, err := pm.HashLockfile(afero.NewOsFs(), filepath.Join(env.SeedRepoPath, "fake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	// Links are written with the hub path resolved (/var -> /private/var).
	hub, err := filepath.EvalSymlinks(env.HubPath)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(hub, "deps")
	install := filepath.Join(store, hash, "node_modules")
	feature := filepath.Join(env.HubPath, "hops", "feature")
	link := filepath.Join(feature, "node_modules")
	if got, err := os.Readlink(filepath.Join(link, "pkg")); err != nil || got != filepath.Join(install, "pkg") {
		t.Fatalf("feature node_modules/pkg = %q (%v), want a link into %q", got, err, install)
	}

	// Put feature back where an earlier release left it.
	oldKey := "node_modules." + hash
	old := filepath.Join(store, oldKey)
	if err := os.MkdirAll(filepath.Join(old, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, filepath.Join(old, "pkg", "index.js"), "old\n")
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, args...)
		out := stdout + stderr
		if code != 0 {
			t.Fatalf("git hop %s: exit %d; output:\n%s", strings.Join(args, " "), code, out)
		}
		return out
	}

	out := run("doctor")
	want := "feature: symlink node_modules -> " + oldKey + " is in the old store layout"
	if !strings.Contains(out, want) {
		t.Errorf("doctor should warn %q; output:\n%s", want, out)
	}

	out = run("env", "gc", "--no-prompt")
	if strings.Contains(out, oldKey) {
		t.Errorf("gc must keep an old install a worktree links; output:\n%s", out)
	}
	if got, err := os.Readlink(link); err != nil || got != old {
		t.Fatalf("gc changed the old link: %q (%v)", got, err)
	}

	run("doctor", "--fix")
	if got, err := os.Readlink(filepath.Join(link, "pkg")); err != nil || got != filepath.Join(install, "pkg") {
		t.Errorf("doctor --fix should relink feature into %q; got %q (%v)", install, got, err)
	}
	if data, err := os.ReadFile(filepath.Join(old, "pkg", "index.js")); err != nil || string(data) != "old\n" {
		t.Errorf("the old install must not be written: %q (%v)", data, err)
	}

	out = run("env", "gc", "--no-prompt")
	if !strings.Contains(out, oldKey) {
		t.Errorf("gc should list the old install once unlinked; output:\n%s", out)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old install should be collected once unlinked; stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(install, "pkg", "index.js")); err != nil {
		t.Errorf("the current install, linked entry by entry, must stay: %v", err)
	}
}
