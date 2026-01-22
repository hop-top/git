package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/output"
	"github.com/spf13/afero"
)

// Clone handles the "Clone Mode" (git hop <uri>)
func Clone(fs afero.Fs, g *git.Git, uri, hubPath string) error {
	// 1. Determine Hub Path
	if hubPath == "" {
		// Derive from repo name
		// URI: git@github.com:org/repo.git -> repo
		// URI: https://github.com/org/repo -> repo
		parts := strings.Split(uri, "/")
		repoName := parts[len(parts)-1]
		repoName = strings.TrimSuffix(repoName, ".git")

		cwd, _ := os.Getwd()
		hubPath = filepath.Join(cwd, repoName)
	}

	// Check if hub exists
	if exists, _ := afero.Exists(fs, hubPath); exists {
		// Allowed if empty or is a hub
		isEmpty, _ := afero.IsEmpty(fs, hubPath)
		if !isEmpty {
			if !IsHub(fs, hubPath) {
				return fmt.Errorf("directory %s exists and is not a git-hop hub", hubPath)
			}
			// If it is a hub, we might be re-cloning? Or just ensuring?
			// For now, fail if exists to be safe, unless force?
			// Spec says: "Allowed if an existing hop hub".
		}
	}

	// 2. Determine Hopspace Location
	// $GIT_HOP_DATA_HOME/<org>/<repo>/
	// We need to parse Org and Repo from URI.
	org, repo := parseURI(uri)
	if org == "" || repo == "" {
		return fmt.Errorf("could not parse org/repo from URI: %s", uri)
	}

	dataHome := os.Getenv("GIT_HOP_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share", "git-hop")
	}
	hopspacePath := GetHopspacePath(dataHome, org, repo)

	output.Info("Cloning %s...", uri)
	output.Info("Hub: %s", hubPath)
	output.Info("Hopspace: %s", hopspacePath)

	// 3. Clone Operation
	// Determine default branch
	defaultBranch, err := g.GetDefaultBranch(uri)
	if err != nil {
		return fmt.Errorf("failed to get default branch: %v", err)
	}
	output.Info("Default branch: %s", defaultBranch)

	// Clone default branch to hopspace
	// $HOPSPACE/<default>
	defaultWorktreePath := filepath.Join(hopspacePath, defaultBranch)

	if err := os.MkdirAll(hopspacePath, 0755); err != nil {
		return fmt.Errorf("failed to create hopspace dir: %v", err)
	}

	if err := g.Clone(uri, defaultWorktreePath, defaultBranch); err != nil {
		return fmt.Errorf("failed to clone: %v", err)
	}

	// 4. Initialize Hub
	if err := os.MkdirAll(hubPath, 0755); err != nil {
		return fmt.Errorf("failed to create hub dir: %v", err)
	}

	// Create Hub Config
	hubCfg := &config.HubConfig{
		Repo: config.RepoConfig{
			URI:           uri,
			Org:           org,
			Repo:          repo,
			DefaultBranch: defaultBranch,
		},
		Branches: make(map[string]config.HubBranch),
	}

	// Add default branch to hub config
	hubCfg.Branches[defaultBranch] = config.HubBranch{
		Path:           defaultBranch,
		HopspaceBranch: defaultBranch,
	}

	// Write Hub Config
	hubWriter := config.NewWriter(fs)
	if err := hubWriter.WriteHubConfig(hubPath, hubCfg); err != nil {
		return fmt.Errorf("failed to write hub config: %v", err)
	}

	// Create .gitignore in Hub
	gitignorePath := filepath.Join(hubPath, ".gitignore")
	gitignoreContent := "*\n!*/\n!.gitignore\n!hop.json\n"
	if err := afero.WriteFile(fs, gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("failed to write .gitignore: %v", err)
	}

	// Create Symlink
	if err := os.Symlink(defaultWorktreePath, filepath.Join(hubPath, defaultBranch)); err != nil {
		return fmt.Errorf("failed to create symlink: %v", err)
	}

	// 5. Initialize Hopspace Config
	hsCfg := &config.HopspaceConfig{
		Repo: config.RepoConfig{
			URI:           uri,
			Org:           org,
			Repo:          repo,
			DefaultBranch: defaultBranch,
		},
		Branches: make(map[string]config.HopspaceBranch),
	}
	hsCfg.Branches[defaultBranch] = config.HopspaceBranch{
		Path:     defaultWorktreePath,
		LastSync: time.Now(),
		Exists:   true,
	}

	if err := hubWriter.WriteHopspaceConfig(hopspacePath, hsCfg); err != nil {
		return fmt.Errorf("failed to write hopspace config: %v", err)
	}

	// 6. Environment Branch Scanning (TODO)
	// "Detect remote branches matching environment patterns..."
	// For now, just the default branch.

	output.Info("Successfully cloned to %s", hubPath)
	return nil
}

func parseURI(uri string) (org, repo string) {
	// git@github.com:org/repo.git
	// https://github.com/org/repo.git
	// https://github.com/org/repo
	// file:///path/to/repo

	trimmed := strings.TrimSuffix(uri, ".git")

	// Handle file:// URIs
	if strings.HasPrefix(trimmed, "file://") {
		// file:///tmp/org/repo -> /tmp/org/repo
		path := strings.TrimPrefix(trimmed, "file://")
		parts := strings.Split(path, "/")
		// Get last two non-empty parts as org/repo
		var nonEmpty []string
		for _, p := range parts {
			if p != "" {
				nonEmpty = append(nonEmpty, p)
			}
		}
		if len(nonEmpty) >= 2 {
			return nonEmpty[len(nonEmpty)-2], nonEmpty[len(nonEmpty)-1]
		} else if len(nonEmpty) == 1 {
			// Single directory name - use it as both org and repo
			return nonEmpty[0], nonEmpty[0]
		}
		return "", ""
	}

	if strings.HasPrefix(trimmed, "git@") {
		// git@host:org/repo
		parts := strings.Split(trimmed, ":")
		if len(parts) == 2 {
			path := parts[1]
			pathParts := strings.Split(path, "/")
			if len(pathParts) >= 2 {
				return pathParts[len(pathParts)-2], pathParts[len(pathParts)-1]
			}
		}
	} else {
		// http(s)://host/org/repo
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	}
	return "", ""
}
