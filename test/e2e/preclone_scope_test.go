package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/hop"
)

// pre-clone resolves at hopspace and global level only. The directory a
// clone runs from, and every ancestor of it, is not part of the repo being
// cloned, so a .git-hop/hooks/pre-clone found there must never execute.

const strayPreCloneHook = `#!/bin/sh
mkdir -p "$GIT_HOP_TEST_MARKER_DIR"
echo fired >> "$GIT_HOP_TEST_MARKER_DIR/stray-pre-clone"
exit 0
`

// plantStrayPreClone writes a pre-clone hook into dir/.git-hop/hooks that
// leaves a marker when it runs.
func plantStrayPreClone(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, ".git-hop", "hooks", "pre-clone")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir stray hooks dir: %v", err)
	}
	WriteFile(t, path, strayPreCloneHook)
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("chmod stray pre-clone: %v", err)
	}
}

func assertStrayNotFired(t *testing.T, env *TestEnv) {
	t.Helper()
	marker := filepath.Join(env.RootDir, "markers", "stray-pre-clone")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("pre-clone hook in the clone's cwd ancestry ran; pre-clone must resolve at hopspace/global only")
	}
}

// cloneFromNestedCwd runs the clone from a directory below RootDir, with a
// stray pre-clone planted both in that cwd and in its ancestor RootDir.
func cloneFromNestedCwd(t *testing.T, env *TestEnv) {
	t.Helper()
	cwd := filepath.Join(env.RootDir, "work")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	plantStrayPreClone(t, cwd)
	plantStrayPreClone(t, env.RootDir)
	env.RunGitHop(t, cwd, env.BareRepoPath, env.HubPath)
}

func TestPreClone_CwdAndAncestorHooksDoNotRun(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)

	cloneFromNestedCwd(t, env)

	assertStrayNotFired(t, env)
}

func TestPreClone_GlobalHookRuns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	writeLifecycleHook(t, env, "pre-clone", "0")

	cloneFromNestedCwd(t, env)

	assertStrayNotFired(t, env)
	assertHookSeq(t, readLifecycleRecords(t, env), []string{"pre-clone"})
}

func TestPreClone_HopspaceHookRuns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)

	org, repo := hop.ParseRepoFromURL(env.BareRepoPath)
	path := filepath.Join(env.DataHome, "github.com", org, repo, "hooks", "pre-clone")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir hopspace hooks dir: %v", err)
	}
	WriteFile(t, path, strings.Replace(recordLifecycleHook, "%s", "0", 1))
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("chmod hopspace pre-clone: %v", err)
	}

	cloneFromNestedCwd(t, env)

	assertStrayNotFired(t, env)
	assertHookSeq(t, readLifecycleRecords(t, env), []string{"pre-clone"})
}
