package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

func TestShorthandClone_E2E(t *testing.T) {
	// Create temp directory for test
	tmpDir := filepath.Join("/tmp", "git-hop-shorthand-test")
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	tests := []struct {
		name           string
		input          string
		gitDomain      string
		expectedURI    string
		shouldExpand   bool
	}{
		{
			name:           "shorthand with default domain",
			input:          "testorg/testrepo",
			gitDomain:      "",
			expectedURI:    "git@github.com:testorg/testrepo.git",
			shouldExpand:   true,
		},
		{
			name:           "shorthand with custom domain",
			input:          "myorg/myrepo",
			gitDomain:      "gitlab.com",
			expectedURI:    "git@gitlab.com:myorg/myrepo.git",
			shouldExpand:   true,
		},
		{
			name:           "full URI not expanded",
			input:          "git@github.com:realorg/realrepo.git",
			gitDomain:      "",
			expectedURI:    "git@github.com:realorg/realrepo.git",
			shouldExpand:   false,
		},
		{
			name:           "branch name not expanded",
			input:          "feat/awesome",
			gitDomain:      "",
			expectedURI:    "feat/awesome",
			shouldExpand:   false,
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
	// Test that expandShorthand works correctly in the context of clone operations
	fs := afero.NewMemMapFs()

	tests := []struct {
		name      string
		input     string
		domain    string
		wantURI   bool
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
			name:    "feat/branch stays as-is",
			input:   "feat/new-feature",
			domain:  "",
			wantURI: false,
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
