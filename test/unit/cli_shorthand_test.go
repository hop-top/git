package hop_test

import (
	"testing"

	"github.com/jadb/git-hop/internal/cli"
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
			name:      "branch name with slashes not expanded",
			input:     "feat/awesome",
			gitDomain: "github.com",
			expected:  "feat/awesome",
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
