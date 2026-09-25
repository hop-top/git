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
	seedNpmRepoWith(t, env, files, nil, branches...)
}

// seedNpmRepoWith is seedNpmRepo cloning the hub with extra clone args.
func seedNpmRepoWith(t *testing.T, env *TestEnv, files map[string]string, cloneArgs []string, branches ...string) {
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
	env.RunGitHop(t, env.RootDir, append([]string{env.BareRepoPath, "hub"}, cloneArgs...)...)
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

// resolveScript prints what each named module resolves to in a worktree,
// MISSING for one that does not resolve.
func resolveScript(names ...string) string {
	return `const r = n => { try { return require(n) } catch (e) { return "MISSING" } };` +
		`console.log(` + strings.Join(func() []string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = `r("` + n + `")`
		}
		return out
	}(), `+" "+`) + `)`
}

// assertNoStoreInstalls fails if any install was put in a deps store under
// root (each has an entry record beside it).
func assertNoStoreInstalls(t *testing.T, root string) {
	t.Helper()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, "node_modules.git-hop.json") {
			t.Errorf("install put in the store: %s", path)
		}
		return nil
	})
}

// npm links file: directory dependencies and workspace packages with
// relative links out of node_modules, which resolve from wherever the
// install is. Such an install stays in the worktree that made it, so each
// worktree resolves exactly what npm resolves there: its own workspace
// packages, and file: paths relative to its own depth.
func TestDeps_NpmLinkedDeps_InstalledPerWorktree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	for name, global := range map[string]bool{"hub store": false, "global store": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := npmTestEnv(t)
			// The lockfile is made at the depth of hops/<branch>, so the
			// file: path reaches the sibling from there.
			env.SeedRepoPath = filepath.Join(env.RootDir, "gen", "hops", "seed")
			initSeed(t, env)
			sibling := filepath.Join(env.RootDir, "fit")
			if err := os.MkdirAll(sibling, 0o755); err != nil {
				t.Fatal(err)
			}
			WriteFile(t, filepath.Join(sibling, "package.json"), `{"name":"fit","version":"1.0.0","main":"index.js"}`)
			WriteFile(t, filepath.Join(sibling, "index.js"), `module.exports = "fit";`+"\n")
			packTarball(t, env, filepath.Join(env.SeedRepoPath, "pkgs"), "a", "a")

			files := map[string]string{
				"package.json": `{"name":"app","version":"1.0.0","private":true,"workspaces":["packages/*"],` +
					`"dependencies":{"a":"file:pkgs/a-1.0.0.tgz","fit":"file:../../../fit"}}`,
				"packages/util/package.json": `{"name":"@app/util","version":"1.0.0","main":"index.js"}`,
				"packages/util/index.js":     `module.exports = "util@seed";` + "\n",
			}
			args := []string{}
			if global {
				args = append(args, "--global")
			}
			seedNpmRepoWith(t, env, files, args, "feat", "fix/deep")
			runHop(t, env, "add", "feat")
			runHop(t, env, "add", "fix/deep")

			want := map[string]string{
				"main":     "a fit util@main",
				"feat":     "a fit util@feat",
				"fix/deep": "a MISSING util@fix/deep", // what npm itself resolves at that depth
			}
			script := resolveScript("a", "fit", "@app/util")
			for branch, expected := range want {
				wt := filepath.Join(env.HubPath, "hops", branch)
				WriteFile(t, filepath.Join(wt, "packages", "util", "index.js"), `module.exports = "util@`+branch+`";`+"\n")
				nm := filepath.Join(wt, "node_modules")
				if info, err := os.Lstat(nm); err != nil || !info.IsDir() {
					t.Errorf("%s: node_modules must be a real directory: %v", branch, err)
				}
				if _, err := os.Stat(filepath.Join(nm, ".git-hop-local")); err != nil {
					t.Errorf("%s: local install not marked: %v", branch, err)
				}
				if got := nodeOutput(t, env, wt, script); got != expected {
					t.Errorf("%s resolves %q, want %q", branch, got, expected)
				}
			}
			assertNoStoreInstalls(t, env.RootDir)

			out := env.RunGitHopCombined(t, env.HubPath, "doctor")
			if !strings.Contains(out, "All dependencies are properly configured") {
				t.Errorf("doctor should accept the local installs; output:\n%s", out)
			}
			runHop(t, env, "doctor", "--fix")
			for branch, expected := range want {
				wt := filepath.Join(env.HubPath, "hops", branch)
				if got := nodeOutput(t, env, wt, script); got != expected {
					t.Errorf("%s after doctor --fix resolves %q, want %q", branch, got, expected)
				}
			}
			if _, err := os.Stat(filepath.Join(env.DataHome, "backups")); err == nil {
				t.Errorf("doctor --fix trashed a local install")
			}

			// npm ci drops the marker with the rest of node_modules: a
			// warning, and --fix marks the install again.
			main := filepath.Join(env.HubPath, "hops", "main")
			env.RunCommand(t, main, "npm", "ci")
			out = env.RunGitHopCombined(t, env.HubPath, "doctor")
			if !strings.Contains(out, "main: local node_modules was not installed by git-hop and cannot be shared") {
				t.Errorf("doctor should warn about the unmarked local install; output:\n%s", out)
			}
			runHop(t, env, "doctor", "--fix")
			if _, err := os.Stat(filepath.Join(main, "node_modules", ".git-hop-local")); err != nil {
				t.Errorf("doctor --fix should mark main's install again: %v", err)
			}
		})
	}
}

// pnpm refuses to install through a link to a directory outside the
// project (ERR_PNPM_UNSAFE_MODULES_DIR), so each worktree gets its own
// pnpm install, where pnpm itself can run.
func TestDeps_Pnpm_InstalledPerWorktree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	env := npmTestEnv(t)
	if _, err := env.RunCommandAllowFail(t, env.RootDir, "pnpm", "--version"); err != nil {
		t.Skip("pnpm not available")
	}
	initSeed(t, env)
	packTarball(t, env, filepath.Join(env.SeedRepoPath, "pkgs"), "a", "a")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "package.json"),
		`{"name":"t","version":"1.0.0","private":true,"dependencies":{"a":"file:pkgs/a-1.0.0.tgz"}}`)
	WriteFile(t, filepath.Join(env.SeedRepoPath, ".gitignore"), "node_modules\n")
	env.RunCommand(t, env.SeedRepoPath, "pnpm", "install")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main", "main:feat")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	runHop(t, env, "add", "feat")

	for _, branch := range []string{"main", "feat"} {
		wt := filepath.Join(env.HubPath, "hops", branch)
		nm := filepath.Join(wt, "node_modules")
		if info, err := os.Lstat(nm); err != nil || !info.IsDir() {
			t.Errorf("%s: node_modules must be a real directory: %v", branch, err)
		}
		if _, err := os.Stat(filepath.Join(nm, ".git-hop-local")); err != nil {
			t.Errorf("%s: local install not marked: %v", branch, err)
		}
		if _, stderr, code := env.RunCommandWithExit(t, wt, "pnpm", "install", "--frozen-lockfile"); code != 0 {
			t.Errorf("%s: pnpm install: exit %d: %s", branch, code, stderr)
		}
		if got := nodeOutput(t, env, wt, `console.log(require("a"))`); got != "a" {
			t.Errorf("%s: require(a) = %s", branch, got)
		}
	}
	assertNoStoreInstalls(t, env.RootDir)
	if out := env.RunGitHopCombined(t, env.HubPath, "doctor"); !strings.Contains(out, "All dependencies are properly configured") {
		t.Errorf("doctor should accept the pnpm installs; output:\n%s", out)
	}
}
