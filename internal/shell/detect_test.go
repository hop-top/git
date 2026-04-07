package shell_test

import (
	"os"
	"testing"

	"hop.top/git/internal/shell"
)

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name     string
		shellEnv string
		expected string
	}{
		{
			name:     "bash shell",
			shellEnv: "/bin/bash",
			expected: "bash",
		},
		{
			name:     "zsh shell",
			shellEnv: "/usr/bin/zsh",
			expected: "zsh",
		},
		{
			name:     "fish shell",
			shellEnv: "/usr/local/bin/fish",
			expected: "fish",
		},
		{
			name:     "unknown shell",
			shellEnv: "/bin/sh",
			expected: "unknown",
		},
		{
			name:     "empty SHELL env",
			shellEnv: "",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original SHELL env
			originalShell := os.Getenv("SHELL")
			defer os.Setenv("SHELL", originalShell)

			// Set test SHELL env
			os.Setenv("SHELL", tt.shellEnv)

			result := shell.DetectShell()
			if result != tt.expected {
				t.Errorf("DetectShell() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIsInteractive(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "interactive terminal",
			envVars:  map[string]string{},
			expected: true, // Default assumption when not in CI
		},
		{
			name: "CI environment",
			envVars: map[string]string{
				"CI": "true",
			},
			expected: false,
		},
		{
			name: "non-interactive flag",
			envVars: map[string]string{
				"HOP_NO_SHELL_INTEGRATION": "1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear inherited env vars that IsInteractive() consults so
			// the test result depends only on the case's envVars map.
			// Without this, running under CI (where CI=true is already
			// set) would make the "interactive terminal" case fail.
			// t.Setenv auto-restores on test completion.
			t.Setenv("CI", "")
			t.Setenv("HOP_NO_SHELL_INTEGRATION", "")

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result := shell.IsInteractive()
			if result != tt.expected {
				t.Errorf("IsInteractive() = %v, want %v", result, tt.expected)
			}
		})
	}
}
