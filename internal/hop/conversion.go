package hop

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

type Converter struct {
	fs         afero.Fs
	git        git.GitInterface
	backupMgr  *BackupManager
	DryRun     bool
	Force      bool
	KeepBackup bool
}

func NewConverter(fs afero.Fs, g git.GitInterface) *Converter {
	return &Converter{
		fs:  fs,
		git: g,
	}
}

func (c *Converter) ConvertToBareWorktree(repoPath string, useBare bool, enforceClean bool) (*config.ConversionResult, error) {
	result := &config.ConversionResult{
		Success:      false,
		CreatedFiles: []string{},
		ModifiedDirs: []string{},
		Errors:       []string{},
		Warnings:     []string{},
	}

	structure := DetectRepoStructure(c.fs, repoPath)
	if structure != config.StandardRepo {
		result.Errors = append(result.Errors, fmt.Sprintf("repository is not a standard git repo (current structure: %s)", structure))
		return result, fmt.Errorf("invalid repository structure")
	}

	currentBranch, err := c.git.GetCurrentBranch(repoPath)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get current branch: %v", err))
		return result, fmt.Errorf("failed to get current branch: %w", err)
	}
	_ = currentBranch

	remoteURL, err := c.git.GetRemoteURL(repoPath)
	if err != nil {
		// No remote configured - use local path for org/repo
		remoteURL = ""
	}

	if enforceClean && useBare {
		status, err := c.git.RunInDir(repoPath, "git", "status", "--porcelain")
		if err == nil && status != "" {
			result.Errors = append(result.Errors, "repository has uncommitted changes (commit or stash before conversion)")
			return result, fmt.Errorf("repository is not clean")
		}
	}

	var org, repo string
	if remoteURL != "" {
		org, repo = parseRepoFromURL(remoteURL)
		if org == "" || repo == "" {
			result.Errors = append(result.Errors, "could not parse org/repo from remote URL")
			return result, fmt.Errorf("invalid remote URL")
		}
	} else {
		// Use repository path for org/repo when no remote
		absPath, err := filepath.Abs(repoPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to get absolute path: %v", err))
			return result, fmt.Errorf("failed to get absolute path: %w", err)
		}
		repo = filepath.Base(absPath)
		org = filepath.Base(filepath.Dir(absPath))
		result.Warnings = append(result.Warnings, "No remote configured - using local path for backup organization")
	}

	c.backupMgr, err = NewBackupManager(c.fs, c.git, org, repo)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to create backup manager: %v", err))
		return result, fmt.Errorf("failed to create backup manager: %w", err)
	}

	if err := c.backupMgr.CreateBackup(repoPath); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to create backup: %v", err))
		return result, fmt.Errorf("backup creation failed: %w", err)
	}

	result.BackupPath = c.backupMgr.GetBackupPath()

	if err := c.performConversion(repoPath, useBare, result); err != nil {
		if c.backupMgr != nil {
			if rollbackErr := c.backupMgr.Restore(repoPath); rollbackErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("rollback failed: %v", rollbackErr))
			}
		}

		result.Errors = append(result.Errors, fmt.Sprintf("conversion failed: %v", err))
		return result, fmt.Errorf("conversion failed: %w", err)
	}

	result.Success = true
	result.ProjectPath = repoPath

	if c.backupMgr.metadata != nil {
		result.Metadata = &config.BackupMetadata{
			Timestamp:     c.backupMgr.metadata.Timestamp,
			OriginalPath:  c.backupMgr.metadata.OriginalPath,
			RemoteUrl:     c.backupMgr.metadata.RemoteUrl,
			CurrentBranch: c.backupMgr.metadata.CurrentBranch,
			Structure:     c.backupMgr.metadata.Structure,
			HasStashes:    c.backupMgr.metadata.HasStashes,
			StashCount:    c.backupMgr.metadata.StashCount,
			GitStatus:     c.backupMgr.metadata.GitStatus,
		}
	}

	result.CreatedFiles = append(result.CreatedFiles, filepath.Join(repoPath, "hop.json"))
	result.ModifiedDirs = append(result.ModifiedDirs, repoPath)

	if !c.KeepBackup {
		if err := c.backupMgr.Cleanup(); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to cleanup backup: %v", err))
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("backup preserved at: %s", c.backupMgr.GetBackupPath()))
	}

	return result, nil
}

func (c *Converter) performConversion(repoPath string, useBare bool, result *config.ConversionResult) error {
	if c.DryRun {
		result.Warnings = append(result.Warnings, "DRY RUN: No changes made")
		return nil
	}

	parentDir := filepath.Dir(repoPath)
	projectName := filepath.Base(repoPath)

	var bareRepoPath string
	if useBare {
		bareRepoPath = repoPath + ".new"
	} else {
		bareRepoPath = repoPath
	}

	if useBare {
		if err := c.git.CloneBare(repoPath, bareRepoPath); err != nil {
			return fmt.Errorf("failed to create bare repository: %w", err)
		}

		currentBranch, _ := c.git.GetCurrentBranch(repoPath)
		_ = currentBranch

		// Create worktrees directory
		worktreesDir := filepath.Join(bareRepoPath, "worktrees")
		if err := c.fs.MkdirAll(worktreesDir, 0755); err != nil {
			return fmt.Errorf("failed to create worktrees directory: %w", err)
		}

		mainPath := filepath.Join(worktreesDir, "main")
		_, err := c.git.Run("git", "-C", bareRepoPath, "worktree", "add", mainPath, "main")
		if err != nil {
			return fmt.Errorf("failed to create main worktree: %w", err)
		}

		if err := c.moveFilesToWorktree(repoPath, mainPath); err != nil {
			return fmt.Errorf("failed to move files to worktree: %w", err)
		}

		if err := c.swapDirectories(parentDir, projectName, bareRepoPath); err != nil {
			return fmt.Errorf("failed to swap directories: %w", err)
		}
	} else {
		// For regular repo conversion, the repo root remains as the working tree
		// for the current branch. We don't create a worktree for it since it's
		// already checked out. We just create the worktrees directory for future branches.
		worktreesDir := filepath.Join(repoPath, "worktrees")
		if err := c.fs.MkdirAll(worktreesDir, 0755); err != nil {
			return fmt.Errorf("failed to create worktrees directory: %w", err)
		}
		// Note: No worktree created for current branch - repo root is its working tree
	}

	worktrees, err := ListWorktrees(c.git, repoPath)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to list worktrees: %v", err))
	} else {
		for _, branch := range worktrees {
			if branch == "main" {
				continue
			}
			worktreePath := c.getWorktreePathForBranch(branch, repoPath)
			if err := c.git.CreateWorktree(repoPath, branch, worktreePath, "", false); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create worktree for %s: %v", branch, err))
			}
		}
	}

	if err := c.createHopConfig(repoPath, useBare, result); err != nil {
		return fmt.Errorf("failed to create hop.json: %w", err)
	}

	return nil
}

// moveFilesToWorktree relocates the source repo's working-tree contents into
// the freshly-created bare worktree. `git worktree add` has already populated
// `dst` with every tracked file from the bare ref, so naively renaming source
// directories on top would collide whenever a tracked subdir exists on both
// sides (e.g. .github/, scripts/). The merge walks each conflicting dir and
// renames children individually, so untracked source content moves over and
// tracked content is left as the worktree placed it. Files always rename
// last-write-wins (source overwrites dest) to preserve any local modifications
// that haven't been committed. T-0166.
func (c *Converter) moveFilesToWorktree(src, dst string) error {
	entries, err := afero.ReadDir(c.fs, src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if err := c.mergeRename(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// mergeRename moves srcPath → dstPath, recursing into sub-entries when the
// destination directory already exists. Files overwrite the destination
// (preserves uncommitted local changes); directories are walked. Empty source
// directories are removed after their contents have moved.
func (c *Converter) mergeRename(srcPath, dstPath string) error {
	srcInfo, err := c.fs.Stat(srcPath)
	if err != nil {
		return err
	}

	dstInfo, dstErr := c.fs.Stat(dstPath)
	if dstErr != nil {
		// Destination missing — straight rename.
		return c.fs.Rename(srcPath, dstPath)
	}

	// Destination exists. If types differ or both are files, overwrite via
	// remove-then-rename so source content wins.
	if !srcInfo.IsDir() || !dstInfo.IsDir() {
		if err := c.fs.RemoveAll(dstPath); err != nil {
			return err
		}
		return c.fs.Rename(srcPath, dstPath)
	}

	// Both are directories — merge children recursively.
	children, err := afero.ReadDir(c.fs, srcPath)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := c.mergeRename(
			filepath.Join(srcPath, child.Name()),
			filepath.Join(dstPath, child.Name()),
		); err != nil {
			return err
		}
	}
	// Source dir is now empty; remove it so swapDirectories sees a clean root.
	if err := c.fs.Remove(srcPath); err != nil {
		return err
	}
	return nil
}

func (c *Converter) swapDirectories(parentDir, projectName, bareRepoPath string) error {
	backupPath := filepath.Join(parentDir, projectName+".old")

	if err := c.fs.Rename(filepath.Join(parentDir, projectName), backupPath); err != nil {
		return err
	}

	if err := c.fs.Rename(bareRepoPath, filepath.Join(parentDir, projectName)); err != nil {
		return err
	}

	if err := c.fs.RemoveAll(backupPath); err != nil {
		return err
	}

	return nil
}

func (c *Converter) getWorktreePathForBranch(branch, repoPath string) string {
	// All worktrees go under hops/ subdirectory
	return filepath.Join(repoPath, "hops", branch)
}

func (c *Converter) createHopConfig(repoPath string, useBare bool, result *config.ConversionResult) error {
	remoteURL, err := c.git.GetRemoteURL(repoPath)
	if err != nil {
		// No remote configured
		remoteURL = ""
	}

	var org, repo string
	if remoteURL != "" {
		org, repo = parseRepoFromURL(remoteURL)
	} else {
		// Use repository path for org/repo when no remote
		absPath, _ := filepath.Abs(repoPath)
		repo = filepath.Base(absPath)
		org = filepath.Base(filepath.Dir(absPath))
	}
	defaultBranch, _ := c.git.GetCurrentBranch(repoPath)

	// For regular repos, the current branch's working tree is the repo root
	// For bare repos, it's in the worktrees directory
	branchPath := config.MakeWorktreePath(defaultBranch)
	structure := "bare-worktree"
	if !useBare {
		branchPath = "."
		structure = "regular-worktree"
	}

	hopConfig := map[string]interface{}{
		"repo": map[string]interface{}{
			"uri":           remoteURL,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     structure,
			"isBare":        useBare,
		},
		"branches": map[string]interface{}{
			defaultBranch: map[string]interface{}{
				"path":   branchPath,
				"exists": true,
			},
		},
		"settings": map[string]interface{}{
			"autoEnvStart": true,
		},
	}

	content, err := json.MarshalIndent(hopConfig, "", "  ")
	if err != nil {
		return err
	}

	configPath := filepath.Join(repoPath, "hop.json")
	if err := afero.WriteFile(c.fs, configPath, content, 0644); err != nil {
		return err
	}

	return nil
}

func (c *Converter) RestoreFromBackup(backupPath, targetPath string) error {
	mgr, err := LoadBackupManager(c.fs, c.git, backupPath)
	if err != nil {
		return fmt.Errorf("failed to load backup manager: %w", err)
	}

	if err := mgr.Restore(targetPath); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	return nil
}

func parseRepoFromURL(uri string) (org, repo string) {
	trimmed := strings.TrimSuffix(uri, ".git")

	// Handle file:// URIs
	if strings.HasPrefix(trimmed, "file://") {
		path := strings.TrimPrefix(trimmed, "file://")
		parts := strings.Split(path, "/")
		var nonEmpty []string
		for _, p := range parts {
			if p != "" {
				nonEmpty = append(nonEmpty, p)
			}
		}
		if len(nonEmpty) >= 2 {
			return nonEmpty[len(nonEmpty)-2], nonEmpty[len(nonEmpty)-1]
		}
		if len(nonEmpty) == 1 {
			return nonEmpty[0], nonEmpty[0]
		}
		return "", ""
	}

	// Handle absolute file paths (e.g., /path/to/repo.git or /tmp/org/repo.git)
	if strings.HasPrefix(trimmed, "/") {
		parts := strings.Split(trimmed, "/")
		var nonEmpty []string
		for _, p := range parts {
			if p != "" {
				nonEmpty = append(nonEmpty, p)
			}
		}
		if len(nonEmpty) >= 2 {
			return nonEmpty[len(nonEmpty)-2], nonEmpty[len(nonEmpty)-1]
		}
		if len(nonEmpty) == 1 {
			return nonEmpty[0], nonEmpty[0]
		}
		return "", ""
	}

	// Handle git@ SSH URIs
	if strings.HasPrefix(trimmed, "git@") {
		parts := strings.Split(trimmed, ":")
		if len(parts) == 2 {
			path := parts[1]
			pathParts := strings.Split(path, "/")
			if len(pathParts) >= 2 {
				return pathParts[len(pathParts)-2], pathParts[len(pathParts)-1]
			}
		}
	}

	// Handle http:// and https:// URIs
	if strings.Contains(trimmed, "://") {
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	}

	return "", ""
}

func ParseRepoFromURL(uri string) (org, repo string) {
	return parseRepoFromURL(uri)
}
