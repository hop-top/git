package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/hop"
)

func TestShorthandClone_E2E(t *testing.T) {
	t.Parallel()
	// This test only exercises pure string helpers (ExpandShorthand/IsURI), so
	// it needs neither a scratch directory nor a CWD change. The former fixed
	// /tmp path was shared process-wide and the os.Chdir mutated global CWD --
	// both unsafe once tests run concurrently, and neither was ever read.

	tests := []struct {
		name         string
		input        string
		gitDomain    string
		expectedURI  string
		shouldExpand bool
	}{
		{
			name:         "shorthand with default domain",
			input:        "testorg/testrepo",
			gitDomain:    "",
			expectedURI:  "git@github.com:testorg/testrepo.git",
			shouldExpand: true,
		},
		{
			name:         "shorthand with custom domain",
			input:        "myorg/myrepo",
			gitDomain:    "gitlab.com",
			expectedURI:  "git@gitlab.com:myorg/myrepo.git",
			shouldExpand: true,
		},
		{
			name:         "full URI not expanded",
			input:        "git@github.com:realorg/realrepo.git",
			gitDomain:    "",
			expectedURI:  "git@github.com:realorg/realrepo.git",
			shouldExpand: false,
		},
		{
			// ExpandShorthand is the no-hub-context fallback and decides on
			// shape alone, so a two-segment name expands here. Whether such a
			// name is actually a branch is settled by ResolveArg against the
			// hub -- see TestSwitchToUnconventionalPrefixWorktree.
			name:         "two-segment name expands without hub context",
			input:        "feat/awesome",
			gitDomain:    "",
			expectedURI:  "git@github.com:feat/awesome.git",
			shouldExpand: true,
		},
		{
			name:         "single-segment name not expanded",
			input:        "main",
			gitDomain:    "",
			expectedURI:  "main",
			shouldExpand: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ExpandShorthand(tt.input, tt.gitDomain)
			if result != tt.expectedURI {
				t.Errorf("expandShorthand(%q, %q) = %q, want %q", tt.input, tt.gitDomain, result, tt.expectedURI)
			}

			// Verify it's a URI if expansion happened
			if tt.shouldExpand {
				if !cli.IsURI(result) {
					t.Errorf("Expected expanded result to be a URI: %q", result)
				}
			}
		})
	}
}

func TestShorthandInCloneContext(t *testing.T) {
	t.Parallel()
	// Test that expandShorthand works correctly in the context of clone operations
	fs := afero.NewMemMapFs()

	tests := []struct {
		name    string
		input   string
		domain  string
		wantURI bool
	}{
		{
			name:    "org/repo becomes URI",
			input:   "anthropics/anthropic-quickstarts",
			domain:  "",
			wantURI: true,
		},
		{
			name:    "branch name stays as-is",
			input:   "main",
			domain:  "",
			wantURI: false,
		},
		{
			// Shape-only without hub context; ResolveArg is what keeps a
			// registered worktree of this name out of clone mode.
			name:    "two-segment name becomes URI without hub context",
			input:   "feat/new-feature",
			domain:  "",
			wantURI: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ExpandShorthand(tt.input, tt.domain)
			gotURI := cli.IsURI(result)

			if gotURI != tt.wantURI {
				t.Errorf("expandShorthand(%q) URI check: got %v, want %v (result: %q)", tt.input, gotURI, tt.wantURI, result)
			}
		})
	}

	// Verify that FindHub still works correctly (related to the parent directory search fix)
	hubPath := "/test/project"
	fs.MkdirAll(hubPath, 0755)
	afero.WriteFile(fs, filepath.Join(hubPath, "hop.json"), []byte(`{"repo":{},"branches":{},"settings":{}}`), 0644)

	// Create a nested directory
	nestedPath := filepath.Join(hubPath, "worktrees", "main", "src")
	fs.MkdirAll(nestedPath, 0755)

	// FindHub should find the hub from nested directory
	foundHub, err := hop.FindHub(fs, nestedPath)
	if err != nil {
		t.Errorf("FindHub failed from nested path: %v", err)
	}
	if foundHub != hubPath {
		t.Errorf("FindHub from nested path: got %q, want %q", foundHub, hubPath)
	}
}

// TestSwitchToUnconventionalPrefixWorktree drives the real binary to prove the
// routing decision end to end: a worktree whose branch prefix is not one of
// the conventional-commit words resolves to a switch, not a clone.
//
// Before the fix, `git hop feature/login` expanded to
// git@github.com:feature/login.git and entered clone mode, because "feature"
// was absent from a hardcoded prefix allowlist. Every prefix nobody
// enumerated -- feature/, release/, hotfix/, personal ones -- hit this.
func TestSwitchToUnconventionalPrefixWorktree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Prefixes deliberately outside the old allowlist, plus one inside it to
	// show the fix did not trade one set of working prefixes for another.
	for _, branch := range []string{"feature/login", "release/2.0", "hotfix/urgent", "jad/spike", "feat/login"} {
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			env := SetupTestEnv(t)
			env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
			env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
			env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
			env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
			env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

			env.RunGitHop(t, env.HubPath, "add", branch)
			env.RunGitHop(t, env.HubPath, "add", "other")

			// `add other` left `current` on the other worktree, so this hop
			// has somewhere to move from.
			stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, branch)
			out := stdout + stderr
			if code != 0 {
				t.Fatalf("`git hop %s` exited %d, want 0 (switch mode)\noutput:\n%s", branch, code, out)
			}
			if strings.Contains(out, "Clone") || strings.Contains(out, "clone") {
				t.Errorf("`git hop %s` routed into clone mode\noutput:\n%s", branch, out)
			}
			if !strings.Contains(out, "Switched to worktree") {
				t.Errorf("`git hop %s` did not report a switch\noutput:\n%s", branch, out)
			}

			// The load-bearing effect: `current` now points at the worktree.
			target, err := os.Readlink(filepath.Join(env.HubPath, "current"))
			if err != nil {
				t.Fatalf("failed to read current symlink: %v", err)
			}
			if want := filepath.Join("hops", branch); target != want {
				t.Errorf("current = %q, want %q", target, want)
			}

			// A clone would have created a directory named after the "org".
			org := strings.SplitN(branch, "/", 2)[0]
			if _, err := os.Stat(filepath.Join(env.HubPath, org)); err == nil {
				t.Errorf("`git hop %s` created %q in the hub -- it cloned instead of switching", branch, org)
			}
		})
	}
}

// TestUnregisteredShorthandStillClones is the other half of the contract: an
// org/repo argument with no matching worktree must still be treated as a clone
// shorthand from inside a hub. Asserted without network access by pointing the
// shorthand at a domain that cannot resolve and checking git-hop got as far as
// attempting the clone against the expanded URI.
func TestUnregisteredShorthandStillClones(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	env.RunGitHop(t, env.HubPath, "add", "feature/login")

	// Not a registered worktree -> shorthand -> clone attempt.
	cmd := exec.Command(env.BinPath, "--git-domain", "invalid.test", "someorg/somerepo")
	cmd.Dir = env.HubPath
	cmd.Env = env.EnvVars
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected clone against an unresolvable domain to fail, got success:\n%s", out)
	}

	text := string(out)
	// Must have been treated as a clone, not rejected as a missing worktree.
	if strings.Contains(text, "does not exist") {
		t.Errorf("unregistered shorthand was treated as a missing worktree instead of a clone\noutput:\n%s", text)
	}
	if !strings.Contains(text, "Clone failed") {
		t.Errorf("expected a clone attempt, got:\n%s", text)
	}
	// And the expansion used the shorthand path, not something else.
	if !strings.Contains(text, "invalid.test") {
		t.Errorf("expected the expanded URI to carry the configured domain\noutput:\n%s", text)
	}
}
