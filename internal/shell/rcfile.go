package shell

import (
	"os"
	"path/filepath"
)

// GetRcFile returns the RC file path for the given shell type
func GetRcFile(shellType string) string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "~"
	}

	switch shellType {
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	default:
		// Default to bash
		return filepath.Join(home, ".bashrc")
	}
}
