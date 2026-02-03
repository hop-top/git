package hop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

func CloneWorktree(fs afero.Fs, g *git.Git, uri, projectPath string, useBare bool, globalConfig bool) error {
	projectRoot := projectPath

	if projectRoot == "" {
		parts := strings.Split(uri, "/")
		repoName := parts[len(parts)-1]
		repoName = strings.TrimSuffix(repoName, ".git")
		cwd, _ := os.Getwd()
		projectRoot = filepath.Join(cwd, repoName)
	}

	org, repo := ParseRepoFromURL(uri)
	if org == "" || repo == "" {
		return fmt.Errorf("could not parse org/repo from URI: %s", uri)
	}

	if exists, _ := afero.DirExists(fs, projectRoot); exists {
		return fmt.Errorf("directory already exists: %s", projectRoot)
	}

	defaultBranch, err := g.GetDefaultBranch(uri)
	if err != nil {
		return fmt.Errorf("failed to get default branch: %v", err)
	}

	fmt.Printf("Cloning %s...\n", uri)
	fmt.Printf("Project root: %s\n", projectRoot)
	fmt.Printf("Default branch: %s\n", defaultBranch)

	if useBare {
		if err := cloneBareRepo(fs, g, uri, projectRoot, defaultBranch); err != nil {
			return err
		}
	} else {
		if err := cloneRegularRepo(fs, g, uri, projectRoot, defaultBranch); err != nil {
			return err
		}
	}

	// All worktrees are under hops/ subdirectory (default pattern)
	mainWorktreePath := filepath.Join(projectRoot, "hops", defaultBranch)

	// Ensure we use absolute path for hopspace registration
	absMainWorktreePath, err := filepath.Abs(mainWorktreePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}

	if globalConfig {
		// Global mode: separate hub and hopspace configs
		if err := createProjectConfig(fs, g, projectRoot, uri, org, repo, defaultBranch, useBare); err != nil {
			return err
		}

		// Initialize hopspace in data directory
		dataHome := GetGitHopDataHome()
		hopspacePath := GetHopspacePath(dataHome, org, repo)
		if err := initializeHopspace(fs, hopspacePath, uri, org, repo, defaultBranch, absMainWorktreePath, true); err != nil {
			return fmt.Errorf("failed to initialize hopspace: %v", err)
		}
	} else {
		// Local mode (default): merged hub+hopspace config in project root
		if err := createMergedConfig(fs, projectRoot, uri, org, repo, defaultBranch, absMainWorktreePath, useBare); err != nil {
			return err
		}
	}

	if err := registerProject(fs, org, repo, defaultBranch, absMainWorktreePath); err != nil {
		fmt.Printf("Warning: failed to register in global registry: %v\n", err)
	}

	// Get relative path from projectRoot to mainWorktreePath for display
	relWorktreePath, err := filepath.Rel(projectRoot, mainWorktreePath)
	if err != nil {
		relWorktreePath = mainWorktreePath
	}
	worktreeDir := filepath.Dir(relWorktreePath)

	fmt.Printf("\nSuccessfully cloned to %s\n", projectRoot)
	fmt.Printf("\nProject structure:\n")
	fmt.Printf("  %s/\n", projectRoot)
	if useBare {
		fmt.Printf("    .git/              (bare repository)\n")
	}
	fmt.Printf("    hop.json\n")
	fmt.Printf("    %s/\n", worktreeDir)
	fmt.Printf("      %s/           (worktree for current branch)\n", defaultBranch)

	fmt.Printf("\nYou can now:\n")
	fmt.Printf("  cd %s         # Work on current branch\n", mainWorktreePath)
	fmt.Printf("  git hop add <branch>  # Add new branch\n")
	fmt.Printf("  git hop <branch>      # Jump to worktree\n")
	fmt.Printf("  git hop              # List all worktrees\n")

	return nil
}

func cloneBareRepo(fs afero.Fs, g *git.Git, uri, projectRoot, defaultBranch string) error {
	fmt.Println("Creating bare repository...")

	if err := g.CloneBare(uri, projectRoot); err != nil {
		return fmt.Errorf("failed to create bare repository: %w", err)
	}

	// Create hops directory
	hopsDir := filepath.Join(projectRoot, "hops")
	if err := fs.MkdirAll(hopsDir, 0755); err != nil {
		return fmt.Errorf("failed to create hops directory: %w", err)
	}

	// Create main worktree under hops/
	mainPath := filepath.Join(hopsDir, defaultBranch)
	_, err := g.Runner.Run("git", "-C", projectRoot, "worktree", "add", mainPath, defaultBranch)
	if err != nil {
		os.RemoveAll(projectRoot)
		return fmt.Errorf("failed to create main worktree: %w", err)
	}

	return nil
}

func cloneRegularRepo(fs afero.Fs, g *git.Git, uri, projectRoot, defaultBranch string) error {
	fmt.Println("Creating regular repository with worktrees...")

	if err := g.Clone(uri, projectRoot, defaultBranch); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	// Create hops directory
	hopsDir := filepath.Join(projectRoot, "hops")
	if err := fs.MkdirAll(hopsDir, 0755); err != nil {
		return fmt.Errorf("failed to create hops directory: %w", err)
	}

	// Create main worktree under hops/
	mainPath := filepath.Join(hopsDir, defaultBranch)
	if err := g.WorktreeAddCreate(projectRoot, defaultBranch, mainPath, defaultBranch); err != nil {
		return fmt.Errorf("failed to create main worktree: %w", err)
	}

	return nil
}

func createProjectConfig(fs afero.Fs, g *git.Git, projectRoot, uri, org, repo, defaultBranch string, useBare bool) error {
	cfgPath := filepath.Join(projectRoot, "hop.json")

	cfg := map[string]any{
		"repo": map[string]any{
			"uri":           uri,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     "bare-worktree",
			"isBare":        useBare,
		},
		"branches": map[string]any{
			defaultBranch: map[string]any{
				"path":           "main",
				"hopspaceBranch": defaultBranch,
			},
		},
		"settings": map[string]any{
			"envPatterns": []string{"dev", "staging", "qa"},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := afero.WriteFile(fs, cfgPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// createMergedConfig creates a single hop.json with both hub and hopspace fields (local mode)
func createMergedConfig(fs afero.Fs, projectRoot, uri, org, repo, defaultBranch, worktreePath string, useBare bool) error {
	cfgPath := filepath.Join(projectRoot, "hop.json")

	cfg := map[string]any{
		"repo": map[string]any{
			"uri":           uri,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     "bare-worktree",
			"isBare":        useBare,
		},
		// Hub branches (points to worktree paths relative to hub)
		"branches": map[string]any{
			defaultBranch: map[string]any{
				"path":           defaultBranch,
				"hopspaceBranch": defaultBranch,
				// Hopspace fields (merged into same branches map)
				"exists":   true,
				"lastSync": time.Now().Format(time.RFC3339),
			},
		},
		"settings": map[string]any{
			"envPatterns": []string{"dev", "staging", "qa"},
		},
		"forks": map[string]any{},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := afero.WriteFile(fs, cfgPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Created local configuration (hub+hopspace) at %s\n", cfgPath)
	return nil
}

func registerProject(fs afero.Fs, org, repo, branch, worktreePath string) error {
	registry := LoadRegistry(fs)
	repoKey := org + "/" + repo

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return err
	}

	if err := registry.AddHop(repoKey, branch, absPath); err != nil {
		return err
	}

	fmt.Printf("Registered in global registry: %s:%s\n", repoKey, branch)

	return nil
}

// initializeHopspace creates the hopspace directory structure and config
func initializeHopspace(fs afero.Fs, hopspacePath, uri, org, repo, defaultBranch, worktreePath string, isGlobal bool) error {
	// Use InitHopspace function which creates the directory and config
	hopspace, err := InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	if err != nil {
		return fmt.Errorf("failed to initialize hopspace: %w", err)
	}

	// Register the initial branch (main) in the hopspace
	if err := hopspace.RegisterBranch(defaultBranch, worktreePath); err != nil {
		return fmt.Errorf("failed to register default branch: %w", err)
	}

	location := "locally"
	if isGlobal {
		location = "globally"
	}
	fmt.Printf("Initialized hopspace %s at %s\n", location, hopspacePath)
	return nil
}
