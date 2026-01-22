package hop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

func CloneWorktree(fs afero.Fs, g *git.Git, uri, projectPath string, useBare bool) error {
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

	if err := createProjectConfig(fs, g, projectRoot, uri, org, repo, defaultBranch, useBare); err != nil {
		return err
	}

	if err := registerProject(fs, org, repo, defaultBranch, filepath.Join(projectRoot, "main")); err != nil {
		fmt.Printf("Warning: failed to register in global registry: %v\n", err)
	}

	fmt.Printf("\nSuccessfully cloned to %s\n", projectRoot)
	fmt.Printf("\nProject structure:\n")
	fmt.Printf("  %s/\n", projectRoot)
	if useBare {
		fmt.Printf("    .git/              (bare repository)\n")
	}
	fmt.Printf("    hop.json\n")
	fmt.Printf("    main/              (worktree for current branch)\n")

	fmt.Printf("\nYou can now:\n")
	fmt.Printf("  cd %s/main          # Work on current branch\n", projectRoot)
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

	_, err := g.Runner.Run("git", "-C", projectRoot, "worktree", "add", "main", defaultBranch)
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

	mainPath := filepath.Join(projectRoot, "main")
	if err := fs.MkdirAll(mainPath, 0755); err != nil {
		return fmt.Errorf("failed to create main directory: %w", err)
	}

	if err := g.WorktreeAddCreate(projectRoot, "main", mainPath, defaultBranch); err != nil {
		return fmt.Errorf("failed to create main worktree: %w", err)
	}

	return nil
}

func createProjectConfig(fs afero.Fs, g *git.Git, projectRoot, uri, org, repo, defaultBranch string, useBare bool) error {
	cfgPath := filepath.Join(projectRoot, "hop.json")

	cfg := map[string]interface{}{
		"repo": map[string]interface{}{
			"uri":           uri,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     "bare-worktree",
			"isBare":        useBare,
		},
		"branches": map[string]interface{}{
			defaultBranch: map[string]interface{}{
				"path":   "main",
				"exists": true,
			},
		},
		"settings": map[string]interface{}{
			"autoEnvStart": true,
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
