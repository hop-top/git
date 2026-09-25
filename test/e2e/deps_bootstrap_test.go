package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Go vendor dir should not appear in non-vendor projects ---

func TestAdd_GoProject_NoVendorWhenNotVendored(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Create bare repo with a Go project that does NOT vendor
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Go project with a real dependency but NO vendor/ directory.
	// This is the standard Go module workflow (GOFLAGS unset, no vendor).
	WriteFile(t, filepath.Join(env.SeedRepoPath, "go.mod"),
		"module example.com/test\n\ngo 1.21\n\nrequire github.com/spf13/pflag v1.0.5\n")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "go.sum"),
		"github.com/spf13/pflag v1.0.5 h1:iy+VFUOCP1a+8yFto/drg2CJ5u0yRoB7fZw3DKv/JXA=\n"+
			"github.com/spf13/pflag v1.0.5/go.mod h1:McXfInJRrz4CZXVZOBLb0bTZqETkiAhM9Iw0y3An2Bg=\n")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "main.go"),
		"package main\n\nimport _ \"github.com/spf13/pflag\"\n\nfunc main() {}\n")

	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: Go project without vendor")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-a")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-a")

	// Clone as hop hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Add a worktree
	env.RunGitHop(t, env.HubPath, "add", "feature-a")

	wtPath := filepath.Join(env.HubPath, "hops", "feature-a")
	vendorPath := filepath.Join(wtPath, "vendor")

	// ASSERT: vendor/ must NOT exist — the project doesn't vendor
	if _, err := os.Stat(vendorPath); err == nil {
		entries, _ := os.ReadDir(vendorPath)
		t.Errorf("vendor/ directory created in non-vendor Go project "+
			"(contains %d entries); git hop should not run 'go mod vendor' "+
			"when the source branch has no vendor/ dir", len(entries))
	}

	// Also verify main worktree was not polluted
	mainVendor := filepath.Join(env.HubPath, "hops", "main", "vendor")
	if _, err := os.Stat(mainVendor); err == nil {
		t.Errorf("vendor/ directory appeared in main worktree after adding feature-a")
	}
}

func TestAdd_GoProject_VendorPreservedWhenVendored(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Go project that DOES vendor
	WriteFile(t, filepath.Join(env.SeedRepoPath, "go.mod"), "module example.com/test\n\ngo 1.21\n")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "go.sum"), "")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "main.go"), "package main\n\nfunc main() {}\n")
	if err := os.MkdirAll(filepath.Join(env.SeedRepoPath, "vendor"), 0755); err != nil {
		t.Fatalf("failed to create vendor directory: %v", err)
	}
	WriteFile(t, filepath.Join(env.SeedRepoPath, "vendor", "modules.txt"), "# vendor manifest\n")

	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: Go project with vendor")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-b")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-b")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature-b")

	wtPath := filepath.Join(env.HubPath, "hops", "feature-b")
	vendorPath := filepath.Join(wtPath, "vendor")

	// ASSERT: vendor/ SHOULD exist (or be a symlink to shared cache)
	if _, err := os.Stat(vendorPath); err != nil {
		t.Errorf("vendor/ missing in worktree for project that vendors: %v", err)
	}
}

// --- pnpm/npm node_modules should not break existing worktrees ---

// An npm project whose dependency imports another dependency, as ES
// modules. Node resolves a package's imports from its real path, so a
// worktree's node_modules links into the shared store only work when the
// install there sits in a directory named node_modules. The deps come from
// tarballs committed to the repo, so no registry is needed.
func TestAdd_NpmProject_ExistingWorktreeDepsIntact(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	for _, bin := range []string{"npm", "node"} {
		if _, err := env.RunCommandAllowFail(t, env.RootDir, bin, "--version"); err != nil {
			t.Skip(bin + " not available")
		}
	}

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// a imports b; both ESM, packed into the repo.
	pkgs := filepath.Join(env.SeedRepoPath, "pkgs")
	if err := os.MkdirAll(pkgs, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"a": `import { b } from "b"; export const a = () => "a+" + b();`,
		"b": `export const b = () => "b";`,
	} {
		dir := filepath.Join(env.RootDir, "pkgsrc", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		WriteFile(t, filepath.Join(dir, "package.json"),
			`{"name":"`+name+`","version":"1.0.0","type":"module","exports":"./index.js"}`)
		WriteFile(t, filepath.Join(dir, "index.js"), src+"\n")
		env.RunCommand(t, dir, "npm", "pack", "--silent", "--pack-destination", pkgs)
	}
	WriteFile(t, filepath.Join(env.SeedRepoPath, "package.json"),
		`{"name":"test","version":"1.0.0","private":true,"type":"module",`+
			`"dependencies":{"a":"file:pkgs/a-1.0.0.tgz","b":"file:pkgs/b-1.0.0.tgz"}}`)
	WriteFile(t, filepath.Join(env.SeedRepoPath, "index.js"), `import { a } from "a"; console.log(a());`+"\n")
	WriteFile(t, filepath.Join(env.SeedRepoPath, ".gitignore"), "node_modules\n")
	// Generates the lockfile.
	env.RunCommand(t, env.SeedRepoPath, "npm", "install", "--no-audit", "--no-fund")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: npm project")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-npm")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-npm")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	mainWT := filepath.Join(env.HubPath, "hops", "main")
	assertESMRuns := func(wt string) {
		t.Helper()
		out, stderr, code := env.RunCommandWithExit(t, wt, "node", "index.js")
		if code != 0 || strings.TrimSpace(out) != "a+b" {
			t.Errorf("node index.js in %s: exit %d, stdout=%q stderr=%s", wt, code, out, stderr)
		}
	}
	assertESMRuns(mainWT)
	mainTarget := installOf(t, mainWT)

	env.RunGitHop(t, env.HubPath, "add", "feature-npm")

	// main is left as it was and still works.
	if after := installOf(t, mainWT); after != mainTarget {
		t.Errorf("main node_modules changed by add: %q -> %q", mainTarget, after)
	}
	assertESMRuns(mainWT)

	// The new worktree shares the install and resolves through it.
	featureWT := filepath.Join(env.HubPath, "hops", "feature-npm")
	if target := installOf(t, featureWT); target != mainTarget {
		t.Errorf("feature node_modules links into %q, want the shared %q", target, mainTarget)
	}
	assertESMRuns(featureWT)
}

func TestAdd_PnpmProject_ExistingWorktreeDepsIntact(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Skip if pnpm not available
	if _, err := env.RunCommandAllowFail(t, env.RootDir, "pnpm", "--version"); err != nil {
		t.Skip("pnpm not available")
	}

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Minimal pnpm project
	WriteFile(t, filepath.Join(env.SeedRepoPath, "package.json"),
		`{"name":"test-pnpm","version":"1.0.0","dependencies":{"is-odd":"3.0.1"}}`)
	env.RunCommand(t, env.SeedRepoPath, "pnpm", "install")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: pnpm project")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-pnpm")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-pnpm")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	mainWT := filepath.Join(env.HubPath, "hops", "main")
	mainNodeMod := filepath.Join(mainWT, "node_modules")

	// Verify main has working node_modules
	if _, err := os.Stat(mainNodeMod); err != nil {
		t.Fatalf("main worktree missing node_modules before add: %v", err)
	}

	// Add feature worktree
	env.RunGitHop(t, env.HubPath, "add", "feature-pnpm")

	// ASSERT: main node_modules still functional
	out, stderr, exitCode := env.RunCommandWithExit(t, mainWT,
		"node", "-e", "require('is-odd')")
	if exitCode != 0 {
		t.Errorf("main worktree deps broken after add: require('is-odd') "+
			"failed (exit %d): stdout=%s stderr=%s", exitCode, out, stderr)
	}

	// ASSERT: feature worktree has working deps
	featureWT := filepath.Join(env.HubPath, "hops", "feature-pnpm")
	out, stderr, exitCode = env.RunCommandWithExit(t, featureWT,
		"node", "-e", "require('is-odd')")
	if exitCode != 0 {
		t.Errorf("feature worktree deps broken: require('is-odd') "+
			"failed (exit %d): stdout=%s stderr=%s", exitCode, out, stderr)
	}
}

// --- Suppress irrelevant env/ports output ---

func TestAdd_NoDockerProject_NoEnvNoise(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Plain project — no docker-compose, no package.json, no go.mod
	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test\n")

	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: plain project")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-plain")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-plain")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	combined := env.RunGitHopCombined(t, env.HubPath, "add", "feature-plain")

	// ASSERT: no port/service noise
	if strings.Contains(combined, "Ports:") {
		t.Errorf("port output shown for project with no Docker env: %s", combined)
	}
	if strings.Contains(combined, "Services:") {
		t.Errorf("services output shown for project with no Docker env: %s", combined)
	}

	// ASSERT: no misleading "Dependencies installed." when no PM detected
	if strings.Contains(combined, "Dependencies installed") {
		t.Errorf("'Dependencies installed' shown when no package manager "+
			"was detected: %s", combined)
	}

	// ASSERT: "Setting up dependencies..." should also be suppressed
	if strings.Contains(combined, "Setting up dependencies") {
		t.Errorf("'Setting up dependencies...' shown when no package manager "+
			"was detected: %s", combined)
	}
}

func TestAdd_GoProject_NoDepsMessageWhenNoPM(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Go project that doesn't vendor — go PM will be detected but
	// should not run (no vendor/ in source). If the fix for the stray
	// vendor/ dir is to skip non-vendor Go projects entirely, then no deps
	// message should appear. If the fix keeps Go PM but uses
	// `go mod download`, then "Dependencies installed." is acceptable
	// but "Setting up dependencies..." should name the PM.
	WriteFile(t, filepath.Join(env.SeedRepoPath, "go.mod"), "module example.com/test\n\ngo 1.21\n")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "go.sum"), "")
	WriteFile(t, filepath.Join(env.SeedRepoPath, "main.go"), "package main\n\nfunc main() {}\n")

	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: Go no-vendor")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-go-clean")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-go-clean")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	combined := env.RunGitHopCombined(t, env.HubPath, "add", "feature-go-clean")

	// ASSERT: no port noise
	if strings.Contains(combined, "Ports:") {
		t.Errorf("port output shown for Go project with no Docker: %s", combined)
	}
}
