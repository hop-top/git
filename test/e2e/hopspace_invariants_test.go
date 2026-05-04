package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// AssertHopspaceShape verifies the contract every freshly-created hopspace
// must satisfy, regardless of which clone path produced it. Use this as a
// post-condition battery in any test that creates a hopspace.
//
// Contract (from the original git-hop design — see CLAUDE.md "Bare Worktree
// Repos"):
//
//   - <hubPath> is a bare git repository — `git rev-parse
//     --is-bare-repository` reports "true".
//   - <hubPath> has no working-tree files leaked from the upstream repo.
//     Only git internals + git-hop additions are allowed at the root.
//   - <hubPath>/hops/<defaultBranch>/ exists, has <defaultBranch> checked
//     out, and tracks origin/<defaultBranch>.
//   - <hubPath>/hops/<defaultBranch>/ is a regular (non-bare) worktree.
//   - `git status` in <hubPath>/hops/<defaultBranch>/ is clean.
func AssertHopspaceShape(t *testing.T, hubPath, defaultBranch string) {
	t.Helper()

	// 1. Hub root is a bare repository.
	out, err := exec.Command("git", "-C", hubPath,
		"rev-parse", "--is-bare-repository").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse --is-bare-repository in %s: %v: %s", hubPath, err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "true" {
		t.Errorf("hub %s must be a bare repo; rev-parse --is-bare-repository = %q", hubPath, got)
	}

	// 2. Hub root contains only git internals + git-hop additions.
	//    No leaked working-tree files from upstream.
	allowed := map[string]bool{
		// git internals (bare layout)
		"HEAD": true, "config": true, "description": true,
		"hooks": true, "info": true, "objects": true, "packed-refs": true,
		"refs": true, "logs": true, "worktrees": true,
		"FETCH_HEAD": true, "ORIG_HEAD": true, "MERGE_HEAD": true,
		"index": true, "shallow": true,
		// git-hop additions
		"hops": true, "hop.json": true, "current": true,
		// idiomatic ignored noise
		".DS_Store": true,
	}
	entries, err := os.ReadDir(hubPath)
	if err != nil {
		t.Fatalf("read hub root %s: %v", hubPath, err)
	}
	for _, e := range entries {
		if allowed[e.Name()] {
			continue
		}
		t.Errorf("hub root must contain only git internals + hops/hop.json/current; "+
			"found leaked entry: %s", e.Name())
	}

	// 3. hops/<defaultBranch>/ exists.
	wt := filepath.Join(hubPath, "hops", defaultBranch)
	info, err := os.Stat(wt)
	if err != nil {
		t.Fatalf("hops/%s missing: %v", defaultBranch, err)
	}
	if !info.IsDir() {
		t.Fatalf("hops/%s must be a directory", defaultBranch)
	}

	// 4. hops/<defaultBranch>/ has <defaultBranch> checked out.
	out, err = exec.Command("git", "-C", wt, "branch", "--show-current").CombinedOutput()
	if err != nil {
		t.Fatalf("branch --show-current in %s: %v: %s", wt, err, out)
	}
	if got := strings.TrimSpace(string(out)); got != defaultBranch {
		t.Errorf("hops/%s: expected branch %q, got %q", defaultBranch, defaultBranch, got)
	}

	// 5. hops/<defaultBranch>/ tracks origin/<defaultBranch>.
	out, err = exec.Command("git", "-C", wt,
		"rev-parse", "--abbrev-ref", defaultBranch+"@{upstream}").CombinedOutput()
	if err != nil {
		t.Errorf("hops/%s has no upstream: %v: %s", defaultBranch, err, out)
	} else if got := strings.TrimSpace(string(out)); got != "origin/"+defaultBranch {
		t.Errorf("hops/%s upstream: expected origin/%s, got %q", defaultBranch, defaultBranch, got)
	}

	// 6. hops/<defaultBranch>/ is a regular (non-bare) worktree.
	out, err = exec.Command("git", "-C", wt,
		"rev-parse", "--is-bare-repository").CombinedOutput()
	if err != nil {
		t.Errorf("rev-parse --is-bare-repository in %s: %v: %s", wt, err, out)
	} else if got := strings.TrimSpace(string(out)); got != "false" {
		t.Errorf("hops/%s must be a regular worktree, got bare=%q", defaultBranch, got)
	}

	// 7. `git status` in hops/<defaultBranch>/ is clean.
	out, err = exec.Command("git", "-C", wt, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Errorf("git status in %s: %v: %s", wt, err, out)
	} else if got := strings.TrimSpace(string(out)); got != "" {
		t.Errorf("hops/%s must have clean working tree; got dirty:\n%s", defaultBranch, got)
	}
}

// TestHopspaceShape_AfterClone runs the invariant battery against a hub
// produced by `git hop <repo>`. This is the single regression guard for
// hopspace shape — covers what TestClone_OutsideRepo only partially asserts.
//
// Captures the contract for T-0215.
func TestHopspaceShape_AfterClone(t *testing.T) {
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	outsideDir := filepath.Join(env.RootDir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if out := env.RunGitHop(t, outsideDir, env.BareRepoPath); !strings.Contains(out, "Successfully cloned") {
		t.Fatalf("git hop clone failed; output:\n%s", out)
	}

	repoName := strings.TrimSuffix(filepath.Base(env.BareRepoPath), ".git")
	hubPath := filepath.Join(outsideDir, repoName)

	AssertHopspaceShape(t, hubPath, "main")
}
