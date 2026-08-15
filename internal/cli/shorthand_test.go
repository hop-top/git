package cli_test

import (
	"testing"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
)

func TestExpandShorthand(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		gitDomain string
		expected  string
	}{
		{
			name:      "full URI remains unchanged",
			input:     "git@github.com:org/repo.git",
			gitDomain: "github.com",
			expected:  "git@github.com:org/repo.git",
		},
		{
			name:      "https URI remains unchanged",
			input:     "https://github.com/org/repo.git",
			gitDomain: "github.com",
			expected:  "https://github.com/org/repo.git",
		},
		{
			name:      "org/repo expands to github.com by default",
			input:     "anthropics/anthropic-quickstarts",
			gitDomain: "",
			expected:  "git@github.com:anthropics/anthropic-quickstarts.git",
		},
		{
			name:      "org/repo expands with custom domain",
			input:     "myorg/myrepo",
			gitDomain: "gitlab.com",
			expected:  "git@gitlab.com:myorg/myrepo.git",
		},
		{
			name:      "org/repo with github.com explicit",
			input:     "testorg/testrepo",
			gitDomain: "github.com",
			expected:  "git@github.com:testorg/testrepo.git",
		},
		{
			name:      "branch name is not expanded",
			input:     "main",
			gitDomain: "github.com",
			expected:  "main",
		},
		{
			// Shape-only fallback: with no hub context, an `a/b` string is
			// indistinguishable from a clone shorthand, so it expands. The
			// branch-vs-clone decision lives in ResolveArg, which consults
			// the hub -- see TestResolveArg.
			name:      "two-segment branch-shaped name expands without hub context",
			input:     "feat/awesome",
			gitDomain: "github.com",
			expected:  "git@github.com:feat/awesome.git",
		},
		{
			name:      "three-segment name not expanded",
			input:     "feature/team/login",
			gitDomain: "github.com",
			expected:  "feature/team/login",
		},
		{
			name:      "path with spaces not expanded",
			input:     "some path",
			gitDomain: "github.com",
			expected:  "some path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ExpandShorthand(tt.input, tt.gitDomain)
			if result != tt.expected {
				t.Errorf("expandShorthand(%q, %q) = %q, want %q", tt.input, tt.gitDomain, result, tt.expected)
			}
		})
	}
}

// TestResolveArg pins the clone-vs-switch routing decision. The hub's
// registered branch set is ground truth: a registered name resolves to a
// switch whatever it looks like, and only an unregistered name falls through
// to shape-based shorthand expansion.
func TestResolveArg(t *testing.T) {
	// A hub whose worktrees deliberately span prefixes the old hardcoded
	// allowlist did and did not contain, plus a name that is also a plausible
	// org/repo shorthand.
	hub := map[string]config.HubBranch{
		"main":           {Path: "/hops/main"},
		"feat/login":     {Path: "/hops/feat/login"},
		"feature/login":  {Path: "/hops/feature/login"},
		"release/2.0":    {Path: "/hops/release/2.0"},
		"hotfix/urgent":  {Path: "/hops/hotfix/urgent"},
		"jad/spike":      {Path: "/hops/jad/spike"},
		"anthropics/sdk": {Path: "/hops/anthropics/sdk"},
	}

	tests := []struct {
		name          string
		arg           string
		gitDomain     string
		knownBranches map[string]config.HubBranch
		expected      string
	}{
		// --- An existing worktree wins over shorthand interpretation. ---
		{
			// The reported bug: `feature` was absent from the old allowlist,
			// so this expanded to git@github.com:feature/login.git and tried
			// to clone instead of switching.
			name:          "registered feature/ worktree switches, not clones",
			arg:           "feature/login",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "feature/login",
		},
		{
			name:          "registered release/ worktree switches",
			arg:           "release/2.0",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "release/2.0",
		},
		{
			name:          "registered hotfix/ worktree switches",
			arg:           "hotfix/urgent",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "hotfix/urgent",
		},
		{
			name:          "registered personal-prefix worktree switches",
			arg:           "jad/spike",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "jad/spike",
		},
		{
			name:          "registered allowlist-era prefix still switches",
			arg:           "feat/login",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "feat/login",
		},
		{
			// A registered worktree beats shorthand even when the name is a
			// perfectly plausible org/repo pair.
			name:          "registered org/repo-shaped worktree switches",
			arg:           "anthropics/sdk",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "anthropics/sdk",
		},
		{
			name:          "registered single-segment worktree switches",
			arg:           "main",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "main",
		},

		// --- org/repo shorthand still clones when no such worktree exists. ---
		{
			name:          "unregistered org/repo clones from inside a hub",
			arg:           "anthropics/anthropic-quickstarts",
			gitDomain:     "",
			knownBranches: hub,
			expected:      "git@github.com:anthropics/anthropic-quickstarts.git",
		},
		{
			// Same prefix as a registered worktree, different repo: still a
			// clone, because this exact name is not registered.
			name:          "unregistered name under a registered prefix clones",
			arg:           "feature/other",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "git@github.com:feature/other.git",
		},
		{
			name:          "unregistered org/repo honors custom domain",
			arg:           "myorg/myrepo",
			gitDomain:     "gitlab.com",
			knownBranches: hub,
			expected:      "git@gitlab.com:myorg/myrepo.git",
		},

		// --- A real URI still clones. ---
		{
			name:          "ssh URI clones unchanged",
			arg:           "git@github.com:org/repo.git",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "git@github.com:org/repo.git",
		},
		{
			name:          "https URI clones unchanged",
			arg:           "https://github.com/org/repo.git",
			gitDomain:     "github.com",
			knownBranches: hub,
			expected:      "https://github.com/org/repo.git",
		},

		// --- Outside a hub, behavior is unchanged. ---
		{
			// nil branch set is the not-in-a-hub signal. Shorthand expansion
			// must behave exactly as it does with no hub context at all.
			name:          "org/repo clones outside a hub",
			arg:           "anthropics/anthropic-quickstarts",
			gitDomain:     "",
			knownBranches: nil,
			expected:      "git@github.com:anthropics/anthropic-quickstarts.git",
		},
		{
			name:          "URI clones outside a hub",
			arg:           "git@github.com:org/repo.git",
			gitDomain:     "",
			knownBranches: nil,
			expected:      "git@github.com:org/repo.git",
		},
		{
			name:          "single-segment name untouched outside a hub",
			arg:           "main",
			gitDomain:     "",
			knownBranches: nil,
			expected:      "main",
		},
		{
			// Empty (but non-nil) hub: a hub with no worktrees registered yet.
			name:          "org/repo clones in an empty hub",
			arg:           "myorg/myrepo",
			gitDomain:     "",
			knownBranches: map[string]config.HubBranch{},
			expected:      "git@github.com:myorg/myrepo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ResolveArg(tt.arg, tt.gitDomain, tt.knownBranches)
			if result != tt.expected {
				t.Errorf("ResolveArg(%q, %q, hub) = %q, want %q", tt.arg, tt.gitDomain, result, tt.expected)
			}
		})
	}
}

// TestResolveArg_RegisteredBranchNeverBecomesURI guards the routing
// consequence directly: the call site branches on IsURI(result), so any
// registered worktree whose resolved form is URI-shaped would be routed into
// clone mode regardless of what the string looks like.
func TestResolveArg_RegisteredBranchNeverBecomesURI(t *testing.T) {
	branches := []string{
		"feature/login", "release/2.0", "hotfix/urgent", "jad/spike",
		"chore/deps", "feat/login", "anthropics/sdk", "main",
	}

	known := make(map[string]config.HubBranch, len(branches))
	for _, b := range branches {
		known[b] = config.HubBranch{Path: "/hops/" + b}
	}

	for _, b := range branches {
		t.Run(b, func(t *testing.T) {
			got := cli.ResolveArg(b, "github.com", known)
			if got != b {
				t.Errorf("ResolveArg(%q) = %q, want %q unchanged", b, got, b)
			}
			if cli.IsURI(got) {
				t.Errorf("ResolveArg(%q) = %q, which IsURI reports true -- would route to clone mode instead of switching", b, got)
			}
		})
	}
}

func TestIsURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "git SSH URI",
			input:    "git@github.com:org/repo.git",
			expected: true,
		},
		{
			name:     "https URI",
			input:    "https://github.com/org/repo.git",
			expected: true,
		},
		{
			name:     "http URI",
			input:    "http://github.com/org/repo.git",
			expected: true,
		},
		{
			name:     ".git suffix",
			input:    "/path/to/repo.git",
			expected: true,
		},
		{
			name:     "org/repo shorthand",
			input:    "org/repo",
			expected: false,
		},
		{
			name:     "branch name",
			input:    "main",
			expected: false,
		},
		{
			name:     "branch with slashes",
			input:    "feat/awesome",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.IsURI(tt.input)
			if result != tt.expected {
				t.Errorf("isURI(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
