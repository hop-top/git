package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// npmTestEnv is a test env with npm and node on PATH, set up to work
// offline: every dependency these tests use is a tarball or a directory
// in the repo.
func npmTestEnv(t *testing.T) *TestEnv {
	t.Helper()
	env := SetupTestEnv(t)
	for _, bin := range []string{"npm", "node"} {
		if _, err := env.RunCommandAllowFail(t, env.RootDir, bin, "--version"); err != nil {
			t.Skip(bin + " not available")
		}
	}
	env.EnvVars = append(env.EnvVars,
		"npm_config_audit=false", "npm_config_fund=false",
		"npm_config_update_notifier=false", "npm_config_offline=true")
	return env
}

// packTarball packs a CommonJS package name exporting value into dest.
func packTarball(t *testing.T, env *TestEnv, dest, name, value string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, "pkgsrc", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, filepath.Join(dir, "package.json"), `{"name":"`+name+`","version":"1.0.0","main":"index.js"}`)
	WriteFile(t, filepath.Join(dir, "index.js"), `module.exports = "`+value+`";`+"\n")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	env.RunCommand(t, dir, "npm", "pack", "--silent", "--pack-destination", dest)
}

// initSeed creates the origin repo and its seed clone.
func initSeed(t *testing.T, env *TestEnv) {
	t.Helper()
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
}

// seedNpmRepo writes files into the seed repo (initSeed), generates the
// lockfile with npm install, pushes main and every branch in branches, and
// clones the hub.
func seedNpmRepo(t *testing.T, env *TestEnv, files map[string]string, branches ...string) {
	t.Helper()
	files[".gitignore"] = "node_modules\n"
	for name, content := range files {
		path := filepath.Join(env.SeedRepoPath, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		WriteFile(t, path, content)
	}
	env.RunCommand(t, env.SeedRepoPath, "npm", "install")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	for _, b := range branches {
		env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main:"+b)
	}
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
}

// nodeOutput runs node script in worktree wt and returns its trimmed
// stdout, or "ERROR: <stderr>" when it fails.
func nodeOutput(t *testing.T, env *TestEnv, wt, script string) string {
	t.Helper()
	out, stderr, code := env.RunCommandWithExit(t, wt, "node", "-e", script)
	if code != 0 {
		return "ERROR: " + stderr
	}
	return strings.TrimSpace(out)
}

// runHop runs git hop in the hub and fails the test on a non-zero exit.
func runHop(t *testing.T, env *TestEnv, args ...string) string {
	t.Helper()
	stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, args...)
	if code != 0 {
		t.Fatalf("git hop %s: exit %d; output:\n%s%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout + stderr
}

// npm ci in a worktree linked to the shared store removes every entry of
// node_modules through the link, emptying the install every other worktree
// links to. doctor reports each worktree left linked to it, and --fix
// reinstalls it.
func TestDeps_NpmCiThroughLink_DoctorReportsFixReinstalls(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	env := npmTestEnv(t)
	initSeed(t, env)
	packTarball(t, env, filepath.Join(env.SeedRepoPath, "pkgs"), "a", "a")
	seedNpmRepo(t, env, map[string]string{
		"package.json": `{"name":"t","version":"1.0.0","private":true,"dependencies":{"a":"file:pkgs/a-1.0.0.tgz"}}`,
	}, "feat", "fix")
	runHop(t, env, "add", "feat")
	runHop(t, env, "add", "fix")

	main := filepath.Join(env.HubPath, "hops", "main")
	feat := filepath.Join(env.HubPath, "hops", "feat")
	fix := filepath.Join(env.HubPath, "hops", "fix")
	install, err := os.Readlink(filepath.Join(main, "node_modules"))
	if err != nil {
		t.Fatalf("main node_modules should link into the store: %v", err)
	}
	const script = `console.log(require("a"))`
	for _, wt := range []string{main, feat, fix} {
		if got := nodeOutput(t, env, wt, script); got != "a" {
			t.Fatalf("%s before npm ci: %s", wt, got)
		}
	}

	env.RunCommand(t, feat, "npm", "ci")
	if got := nodeOutput(t, env, main, script); got == "a" {
		t.Fatalf("expected npm ci in feat to empty main's install at %s", install)
	}

	// doctor exits 1 while it finds issues.
	out := env.RunGitHopCombined(t, env.HubPath, "doctor")
	for _, branch := range []string{"main", "fix"} {
		want := branch + ": shared install " + filepath.Base(filepath.Dir(install)) + "/node_modules is missing entries"
		if !strings.Contains(out, want) {
			t.Errorf("doctor should report %q; output:\n%s", want, out)
		}
	}

	runHop(t, env, "doctor", "--fix")
	for _, wt := range []string{main, feat, fix} {
		if got, err := os.Readlink(filepath.Join(wt, "node_modules")); err != nil || got != install {
			t.Errorf("%s node_modules = %q (%v), want link to %q", wt, got, err, install)
		}
		if got := nodeOutput(t, env, wt, script); got != "a" {
			t.Errorf("%s after doctor --fix: %s", wt, got)
		}
	}
	if out := runHop(t, env, "doctor"); strings.Contains(out, "missing entries") {
		t.Errorf("doctor after --fix still reports damage:\n%s", out)
	}
}
