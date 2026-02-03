package hop

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
)

// ParseRepositoryIDFromURL extracts the repository ID from a Git remote URL
// Supports both SSH and HTTPS formats
// Returns format: domain/org/repo
func ParseRepositoryIDFromURL(remoteURL string) string {
	// SSH format: git@github.com:org/repo.git
	sshRegex := regexp.MustCompile(`git@([^:]+):([^/]+)/([^\.]+)`)
	if matches := sshRegex.FindStringSubmatch(remoteURL); len(matches) == 4 {
		domain := matches[1]
		org := matches[2]
		repo := matches[3]
		return fmt.Sprintf("%s/%s/%s", domain, org, repo)
	}

	// HTTPS format: https://github.com/org/repo.git
	httpsRegex := regexp.MustCompile(`https://([^/]+)/([^/]+)/([^\.]+)`)
	if matches := httpsRegex.FindStringSubmatch(remoteURL); len(matches) == 4 {
		domain := matches[1]
		org := matches[2]
		repo := matches[3]
		return fmt.Sprintf("%s/%s/%s", domain, org, repo)
	}

	return ""
}

// GetRepositoryIDFromPath extracts org/repo from a filesystem path
// Uses the last two path components
// Returns format: domain/org/repo
func GetRepositoryIDFromPath(path string, defaultDomain string) string {
	// Clean and split the path
	cleanPath := filepath.Clean(path)
	parts := strings.Split(cleanPath, string(filepath.Separator))

	// Get the last two non-empty parts
	var repo, org string
	for i := len(parts) - 1; i >= 0 && (repo == "" || org == ""); i-- {
		if parts[i] != "" {
			if repo == "" {
				repo = parts[i]
			} else if org == "" {
				org = parts[i]
			}
		}
	}

	if org == "" || repo == "" {
		return ""
	}

	return fmt.Sprintf("%s/%s/%s", defaultDomain, org, repo)
}

// worktreeExists checks if a worktree path exists on the filesystem
func worktreeExists(fs afero.Fs, path string) bool {
	exists, err := afero.DirExists(fs, path)
	return err == nil && exists
}
