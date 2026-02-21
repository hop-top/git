package shell_test

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/shell"
)

func TestGetRcFile(t *testing.T) {
	// Save and restore HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	testHome := "/home/testuser"
	os.Setenv("HOME", testHome)

	tests := []struct {
		name     string
		shellType string
		expected string
	}{
		{
			name:      "bash rc file",
			shellType: "bash",
			expected:  filepath.Join(testHome, ".bashrc"),
		},
		{
			name:      "zsh rc file",
			shellType: "zsh",
			expected:  filepath.Join(testHome, ".zshrc"),
		},
		{
			name:      "fish rc file",
			shellType: "fish",
			expected:  filepath.Join(testHome, ".config", "fish", "config.fish"),
		},
		{
			name:      "unknown shell defaults to bashrc",
			shellType: "unknown",
			expected:  filepath.Join(testHome, ".bashrc"),
		},
		{
			name:      "empty shell type defaults to bashrc",
			shellType: "",
			expected:  filepath.Join(testHome, ".bashrc"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shell.GetRcFile(tt.shellType)
			if result != tt.expected {
				t.Errorf("GetRcFile(%q) = %q, want %q", tt.shellType, result, tt.expected)
			}
		})
	}
}

func TestGetRcFileWithCustomHome(t *testing.T) {
	// Save and restore HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	customHome := "/custom/home/dir"
	os.Setenv("HOME", customHome)

	result := shell.GetRcFile("bash")
	expected := filepath.Join(customHome, ".bashrc")

	if result != expected {
		t.Errorf("GetRcFile with custom HOME = %q, want %q", result, expected)
	}
}
