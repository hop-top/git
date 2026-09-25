package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// packPackage packs a package named name with the given files (package.json
// included) into dest.
func packPackage(t *testing.T, env *TestEnv, dest, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, "pkgsrc", strings.ReplaceAll(name, "/", "-"))
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		WriteFile(t, path, content)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	env.RunCommand(t, dir, "npm", "pack", "--silent", "--pack-destination", dest)
}

// seedEntriesRepo seeds a repo whose dependencies exercise Node resolution
// through links: CommonJS packages, a scoped package requiring a sibling,
// an ESM package importing an undeclared sibling with a command in .bin,
// and a package requiring its peer. Clones the hub, adds feat and fix.
func seedEntriesRepo(t *testing.T, env *TestEnv) {
	t.Helper()
	initSeed(t, env)
	pkgs := filepath.Join(env.SeedRepoPath, "pkgs")
	packTarball(t, env, pkgs, "a", "a")
	packTarball(t, env, pkgs, "b", "b")
	packTarball(t, env, pkgs, "p", "p")
	packPackage(t, env, pkgs, "@s/x", map[string]string{
		"package.json": `{"name":"@s/x","version":"1.0.0","main":"index.js"}`,
		"index.js":     `module.exports = "x+" + require("a");` + "\n",
	})
	packPackage(t, env, pkgs, "pc", map[string]string{
		"package.json": `{"name":"pc","version":"1.0.0","main":"index.js","peerDependencies":{"p":"*"}}`,
		"index.js":     `module.exports = require("p");` + "\n",
	})
	packPackage(t, env, pkgs, "esm", map[string]string{
		"package.json": `{"name":"esm","version":"1.0.0","type":"module","exports":"./index.js","bin":{"esmcli":"./cli.js"}}`,
		"index.js":     `import b from "b"; export default "esm+" + b;` + "\n",
		"cli.js":       "#!/usr/bin/env node\nimport e from \"esm\"; console.log(\"cli:\" + e);\n",
	})
	deps := []string{}
	for _, dep := range [][2]string{{"a", "a"}, {"b", "b"}, {"p", "p"}, {"pc", "pc"}, {"esm", "esm"}, {"@s/x", "s-x"}} {
		deps = append(deps, `"`+dep[0]+`":"file:pkgs/`+dep[1]+`-1.0.0.tgz"`)
	}
	seedNpmRepo(t, env, map[string]string{
		"package.json": `{"name":"t","version":"1.0.0","private":true,"dependencies":{` + strings.Join(deps, ",") + `}}`,
	}, "feat", "fix")
	runHop(t, env, "add", "feat")
	runHop(t, env, "add", "fix")
}

// assertResolves checks that everything in wt resolves through its
// node_modules: CommonJS, a scoped package's sibling, a peer (one
// instance), an ESM import of an undeclared sibling, and a .bin command.
func assertResolves(t *testing.T, env *TestEnv, wt, when string) {
	t.Helper()
	cjs := `console.log(require("a"), require("@s/x"), require("pc") === require("p"))`
	if got := nodeOutput(t, env, wt, cjs); got != "a x+a true" {
		t.Errorf("%s %s: CommonJS resolves %q", filepath.Base(wt), when, got)
	}
	out, stderr, code := env.RunCommandWithExit(t, wt, "node", "--input-type=module", "-e", `import e from "esm"; console.log(e)`)
	if code != 0 || strings.TrimSpace(out) != "esm+b" {
		t.Errorf("%s %s: ESM import: exit %d, %q %s", filepath.Base(wt), when, code, out, stderr)
	}
	out, stderr, code = env.RunCommandWithExit(t, wt, filepath.Join(wt, "node_modules", ".bin", "esmcli"))
	if code != 0 || strings.TrimSpace(out) != "cli:esm+b" {
		t.Errorf("%s %s: .bin/esmcli: exit %d, %q %s", filepath.Base(wt), when, code, out, stderr)
	}
}

// assertEntryLayout checks that wt's node_modules is a real directory
// linking each package into install.
func assertEntryLayout(t *testing.T, wt, install string) {
	t.Helper()
	nm := filepath.Join(wt, "node_modules")
	if info, err := os.Lstat(nm); err != nil || !info.IsDir() {
		t.Fatalf("%s: node_modules must be a real directory: %v", wt, err)
	}
	for _, rel := range []string{"a", "esm", "@s/x", ".bin/esmcli"} {
		if got, err := os.Readlink(filepath.Join(nm, rel)); err != nil || got != filepath.Join(install, rel) {
			t.Errorf("%s: node_modules/%s -> %q (%v), want a link into %s", filepath.Base(wt), rel, got, err, install)
		}
	}
	if info, err := os.Lstat(filepath.Join(nm, ".package-lock.json")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("%s: npm's hidden lockfile must be a copy: %v", filepath.Base(wt), err)
	}
}

// installOf returns the store install wt's node_modules links into.
func installOf(t *testing.T, wt string) string {
	t.Helper()
	link, err := os.Readlink(filepath.Join(wt, "node_modules", "a"))
	if err != nil {
		t.Fatalf("%s: node_modules/a must be a link into the store: %v", wt, err)
	}
	return filepath.Dir(link)
}

// npm ci in one worktree removes that worktree's links and installs there;
// the shared install and every other worktree keep working. npm install
// replaces a worktree's links with packages, and rm -rf node_modules/*
// removes them; doctor reports each, and doctor --fix relinks them all.
func TestDepsEntries_NpmCiLeavesOtherWorktreesWorking(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	env := npmTestEnv(t)
	seedEntriesRepo(t, env)
	main := filepath.Join(env.HubPath, "hops", "main")
	feat := filepath.Join(env.HubPath, "hops", "feat")
	fix := filepath.Join(env.HubPath, "hops", "fix")
	install := installOf(t, main)
	for _, wt := range []string{main, feat, fix} {
		assertEntryLayout(t, wt, install)
		assertResolves(t, env, wt, "after add")
	}

	before := treeDigest(t, install)
	env.RunCommand(t, feat, "npm", "ci")
	for _, wt := range []string{main, feat, fix} {
		assertResolves(t, env, wt, "after npm ci in feat")
	}
	out := env.RunGitHopCombined(t, env.HubPath, "doctor")
	if !strings.Contains(out, "feat: local node_modules") {
		t.Errorf("doctor should report feat's own install as a local folder; output:\n%s", out)
	}
	if strings.Contains(out, "missing entries") || strings.Contains(out, "main:") || strings.Contains(out, "fix:") {
		t.Errorf("doctor should find nothing wrong with main and fix; output:\n%s", out)
	}

	// npm install replaces fix's links with package directories, keeping
	// git-hop's record: a local folder too.
	env.RunCommand(t, fix, "npm", "install")
	assertResolves(t, env, main, "after npm install in fix")
	out = env.RunGitHopCombined(t, env.HubPath, "doctor")
	if !strings.Contains(out, "fix: local node_modules") {
		t.Errorf("doctor should report fix's own install as a local folder; output:\n%s", out)
	}
	if got := treeDigest(t, install); got != before {
		t.Errorf("the shared install changed:\n%s\nwant:\n%s", got, before)
	}

	entries, err := os.ReadDir(filepath.Join(fix, "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			if err := os.RemoveAll(filepath.Join(fix, "node_modules", e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertResolves(t, env, main, "after rm -rf node_modules/* in fix")
	out = env.RunGitHopCombined(t, env.HubPath, "doctor")
	if want := "fix: node_modules is missing links into shared install " + filepath.Base(filepath.Dir(install)) + "/node_modules"; !strings.Contains(out, want) {
		t.Errorf("doctor should report %q; output:\n%s", want, out)
	}

	runHop(t, env, "doctor", "--fix")
	for _, wt := range []string{main, feat, fix} {
		assertEntryLayout(t, wt, install)
		assertResolves(t, env, wt, "after doctor --fix")
	}
	if out := runHop(t, env, "doctor"); !strings.Contains(out, "All dependencies are properly configured") {
		t.Errorf("doctor after --fix should be clean; output:\n%s", out)
	}
}

// treeDigest lists every path below root with its size (links with their
// targets), to show a tree was not changed.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			lines = append(lines, rel+" -> "+target)
			return nil
		}
		lines = append(lines, rel+" "+info.Mode().String()+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// Worktrees an earlier release linked to the shared install as a whole:
// doctor warns, and doctor --fix converts them to per-entry links into the
// same install without reinstalling or writing it. npm ci in one of them
// then leaves the other working.
func TestDepsEntries_SingleLinkMigratedByDoctorFix(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	env := npmTestEnv(t)
	seedEntriesRepo(t, env)
	main := filepath.Join(env.HubPath, "hops", "main")
	feat := filepath.Join(env.HubPath, "hops", "feat")
	install := installOf(t, main)
	for _, wt := range []string{main, feat} {
		if err := os.RemoveAll(filepath.Join(wt, "node_modules")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(install, filepath.Join(wt, "node_modules")); err != nil {
			t.Fatal(err)
		}
		assertResolves(t, env, wt, "through a single link")
	}
	before := treeDigest(t, install)
	pkgJSON := filepath.Join(install, "a", "package.json")
	original, err := os.Stat(pkgJSON)
	if err != nil {
		t.Fatal(err)
	}

	out := runHop(t, env, "doctor")
	for _, branch := range []string{"main", "feat"} {
		want := branch + ": node_modules is a single link to shared install " + filepath.Base(filepath.Dir(install)) + "/node_modules"
		if !strings.Contains(out, want) {
			t.Errorf("doctor should warn %q; output:\n%s", want, out)
		}
	}

	runHop(t, env, "doctor", "--fix")
	if now, err := os.Stat(pkgJSON); err != nil || !os.SameFile(original, now) {
		t.Errorf("doctor --fix reinstalled the shared install (%v)", err)
	}
	for _, wt := range []string{main, feat} {
		assertEntryLayout(t, wt, install)
		assertResolves(t, env, wt, "after doctor --fix")
	}
	if got := treeDigest(t, install); got != before {
		t.Errorf("the conversion changed the shared install:\n%s\nwant:\n%s", got, before)
	}
	if out := runHop(t, env, "doctor"); !strings.Contains(out, "All dependencies are properly configured") {
		t.Errorf("doctor after --fix should be clean; output:\n%s", out)
	}

	env.RunCommand(t, feat, "npm", "ci")
	assertResolves(t, env, main, "after npm ci in feat")
	if got := treeDigest(t, install); got != before {
		t.Errorf("npm ci in feat changed the shared install")
	}
}
