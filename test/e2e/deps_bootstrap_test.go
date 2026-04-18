package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- T-0087: Go vendor dir should not appear in non-vendor projects ---

func TestAdd_GoProject_NoVendorWhenNotVendored(t *testing.T) {
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

// --- T-0088: pnpm/npm node_modules should not break existing worktrees ---

func TestAdd_NpmProject_ExistingWorktreeDepsIntact(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	// Skip if npm not available
	if _, err := env.RunCommandAllowFail(t, env.RootDir, "npm", "--version"); err != nil {
		t.Skip("npm not available")
	}

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Minimal npm project
	WriteFile(t, filepath.Join(env.SeedRepoPath, "package.json"),
		`{"name":"test","version":"1.0.0","dependencies":{"is-odd":"3.0.1"}}`)
	// Run npm install to generate lockfile
	env.RunCommand(t, env.SeedRepoPath, "npm", "install")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "init: npm project")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature-npm")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature-npm")

	// Clone and setup
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	mainWT := filepath.Join(env.HubPath, "hops", "main")

	// Verify main worktree has working node_modules before adding feature
	mainNodeMod := filepath.Join(mainWT, "node_modules")
	mainModuleBefore, err := os.Lstat(mainNodeMod)
	if err != nil {
		t.Fatalf("main worktree missing node_modules before add: %v", err)
	}
	mainIsSymlinkBefore := mainModuleBefore.Mode()&os.ModeSymlink != 0

	// Now add the feature worktree
	env.RunGitHop(t, env.HubPath, "add", "feature-npm")

	// ASSERT: main worktree node_modules still works after adding feature
	mainModuleAfter, err := os.Stat(mainNodeMod)
	if err != nil {
		t.Errorf("main worktree node_modules broken after adding feature: %v", err)
	} else {
		// If it was a symlink, verify target still exists
		if mainIsSymlinkBefore || mainModuleAfter.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(mainNodeMod)
			if err == nil {
				// Resolve relative symlink targets against parent dir
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(mainNodeMod), target)
				}
				if _, err := os.Stat(target); err != nil {
					t.Errorf("main worktree node_modules symlink target "+
						"broken after adding feature: %s -> %s: %v",
						mainNodeMod, target, err)
				}
			}
		}
	}

	// ASSERT: feature worktree has working node_modules
	featureWT := filepath.Join(env.HubPath, "hops", "feature-npm")
	featureNodeMod := filepath.Join(featureWT, "node_modules")
	if _, err := os.Stat(featureNodeMod); err != nil {
		t.Errorf("feature worktree missing node_modules: %v", err)
	}

	// ASSERT: can actually require a module in the feature worktree
	out, stderr, exitCode := env.RunCommandWithExit(t, featureWT,
		"node", "-e", "require('is-odd')")
	if exitCode != 0 {
		t.Errorf("node require('is-odd') failed in feature worktree: stdout=%s stderr=%s", out, stderr)
	}
}

func TestAdd_PnpmProject_ExistingWorktreeDepsIntact(t *testing.T) {
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

// --- T-0089: Suppress irrelevant env/ports output ---

func TestAdd_NoDockerProject_NoEnvNoise(t *testing.T) {
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
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Go project that doesn't vendor — go PM will be detected but
	// should not run (no vendor/ in source). If the fix for T-0087
	// is to skip non-vendor Go projects entirely, then no deps
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
