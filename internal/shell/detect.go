package shell

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectShell returns the current shell type (bash, zsh, fish, or unknown)
func DetectShell() string {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		return "unknown"
	}

	shellName := filepath.Base(shellPath)

	switch {
	case strings.Contains(shellName, "bash"):
		return "bash"
	case strings.Contains(shellName, "zsh"):
		return "zsh"
	case strings.Contains(shellName, "fish"):
		return "fish"
	default:
		return "unknown"
	}
}

// IsInteractive returns true if the current environment is interactive
// (not CI, not piped, not non-interactive mode)
func IsInteractive() bool {
	// Check for CI environment
	if os.Getenv("CI") != "" {
		return false
	}

	// Check for explicit non-interactive flag
	if os.Getenv("HOP_NO_SHELL_INTEGRATION") != "" {
		return false
	}

	// TODO: Check if stdin/stdout are terminals
	// For now, assume interactive if not in CI
	return true
}
