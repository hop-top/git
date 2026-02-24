package services

import (
	"fmt"
	"os"
	"path/filepath"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

// DepsManager manages shared dependencies across worktrees
type DepsManager struct {
	Registry        *DepsRegistry
	RepoPath        string
	PackageManagers []PackageManager
	HopspaceConfig  *config.HopspaceConfig // For resolving install command overrides
	fs              afero.Fs
	trash           *Trash
}

// IssueType represents the type of dependency issue
type IssueType string

const (
	IssueLocalFolder   IssueType = "local_folder"
	IssueBrokenSymlink IssueType = "broken_symlink"
	IssueStaleSymlink  IssueType = "stale_symlink"
	IssueMissingDeps   IssueType = "missing_deps"
)

// Issue represents a dependency issue found during audit
type Issue struct {
	Type          IssueType
	WorktreePath  string
	Branch        string
	PM            PackageManager
	CurrentHash   string
	ExpectedHash  string
	DepsKey       string
	SymlinkTarget string
	Size          int64
}

// NewDepsManager creates a new dependency manager
func NewDepsManager(fs afero.Fs, repoPath string, globalConfig *config.GlobalConfig) (*DepsManager, error) {
	// Load package managers
	pms, err := LoadPackageManagers(globalConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load package managers: %w", err)
	}

	// Load registry
	registry, err := LoadRegistry(fs, repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	// Load hopspace config (for install command overrides)
	loader := config.NewLoader(fs)
	hopspaceConfig, err := loader.LoadHopspaceConfig(repoPath)
	if err != nil {
		// Hopspace config is optional, continue without it
		hopspaceConfig = nil
	}

	return &DepsManager{
		Registry:        registry,
		RepoPath:        repoPath,
		PackageManagers: pms,
		HopspaceConfig:  hopspaceConfig,
		fs:              fs,
		trash:           NewTrash(fs),
	}, nil
}

// NewDepsManagerFromParts creates a DepsManager from pre-built components.
// This is primarily useful in tests where commands are not available on PATH.
func NewDepsManagerFromParts(fs afero.Fs, repoPath string, registry *DepsRegistry, pms []PackageManager, hopspaceConfig *config.HopspaceConfig) *DepsManager {
	return &DepsManager{
		Registry:        registry,
		RepoPath:        repoPath,
		PackageManagers: pms,
		HopspaceConfig:  hopspaceConfig,
		fs:              fs,
		trash:           NewTrash(fs),
	}
}

// EnsureDeps ensures dependencies are set up for a worktree
func (m *DepsManager) EnsureDeps(worktreePath, branch string) error {
	// Detect package managers in this worktree
	detectedPMs, err := DetectPackageManagers(m.fs, worktreePath, m.PackageManagers)
	if err != nil {
		return fmt.Errorf("failed to detect package managers: %w", err)
	}

	// For each detected PM, ensure deps are installed and symlinked
	for _, pm := range detectedPMs {
		if err := m.ensurePMDeps(worktreePath, branch, pm); err != nil {
			return fmt.Errorf("failed to ensure deps for %s: %w", pm.Name, err)
		}
	}

	// Save registry
	if err := m.Registry.Save(m.fs, m.RepoPath); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	return nil
}

// ensurePMDeps ensures deps for a specific package manager
func (m *DepsManager) ensurePMDeps(worktreePath, branch string, pm PackageManager) error {
	lockfilePath, err := pm.FindLockfile(m.fs, worktreePath)
	if err != nil {
		return nil
	}

	// Resolve install command with hierarchy (branch > repo > global)
	resolvedPM := ResolveInstallCmd(pm, m.HopspaceConfig, branch)

	// Compute hash
	hash, err := resolvedPM.HashLockfile(m.fs, lockfilePath)
	if err != nil {
		return fmt.Errorf("failed to hash lockfile: %w", err)
	}

	depsKey := resolvedPM.GetDepsKey(hash)
	depsPath := m.getDepsPath(depsKey)
	symlinkPath := filepath.Join(worktreePath, resolvedPM.DepsDir)

	// Check if deps already installed
	depsExists, err := afero.DirExists(m.fs, depsPath)
	if err != nil {
		return fmt.Errorf("failed to check deps existence: %w", err)
	}

	if !depsExists {
		// Install deps to shared storage using resolved PM
		if err := m.installDeps(depsPath, worktreePath, *resolvedPM); err != nil {
			return fmt.Errorf("failed to install deps: %w", err)
		}

		// Update registry metadata
		m.Registry.UpdateEntryMetadata(depsKey, hash, filepath.Base(lockfilePath))
	}

	// Check if something exists at the symlink path.
	// ReadlinkIfPossible is checked first because afero.Exists follows symlinks
	// and returns false for dangling symlinks, causing createSymlink to fail
	// with "file exists" when the dangling symlink is still on disk.
	isSymlink := false
	var currentTarget string
	if linker, ok := m.fs.(afero.Symlinker); ok {
		target, err := linker.ReadlinkIfPossible(symlinkPath)
		if err == nil {
			isSymlink = true
			currentTarget = target
		}
	}

	if isSymlink {
		if currentTarget == depsPath {
			// Symlink is correct, just update usage
			m.Registry.AddUsage(depsKey, branch)
			return nil
		}
		// Symlink points to wrong or dangling target, remove it
		if err := m.fs.Remove(symlinkPath); err != nil {
			return fmt.Errorf("failed to remove stale symlink: %w", err)
		}
	} else {
		// Check if a real directory exists at the path
		symlinkExists, err := afero.Exists(m.fs, symlinkPath)
		if err != nil {
			return fmt.Errorf("failed to check symlink existence: %w", err)
		}
		if symlinkExists {
			// Real directory - move to trash
			if _, err := m.trash.Move(symlinkPath); err != nil {
				return fmt.Errorf("failed to trash local deps: %w", err)
			}
		}
	}

	// Create symlink
	if err := m.createSymlink(depsPath, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	// Add usage to registry
	m.Registry.AddUsage(depsKey, branch)

	return nil
}

// installDeps installs dependencies to the target directory
func (m *DepsManager) installDeps(targetDir, worktreePath string, pm PackageManager) error {
	// Create target directory
	if err := m.fs.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Run install command
	if err := pm.Install(targetDir, worktreePath); err != nil {
		// Clean up on failure
		m.fs.RemoveAll(targetDir)
		return fmt.Errorf("failed to run install: %w", err)
	}

	return nil
}

// createSymlink creates a symlink from source to target
func (m *DepsManager) createSymlink(target, linkPath string) error {
	if linker, ok := m.fs.(afero.Symlinker); ok {
		if err := linker.SymlinkIfPossible(target, linkPath); err != nil {
			return fmt.Errorf("failed to create symlink: %w", err)
		}
		return nil
	}
	return fmt.Errorf("filesystem does not support symlinks")
}

// Audit scans all worktrees and identifies dependency issues.
// worktrees is a map of branchName → worktreePath.
func (m *DepsManager) Audit(worktrees map[string]string) ([]Issue, error) {
	issues := []Issue{}

	// Rebuild registry from worktrees first
	if err := m.Registry.RebuildFromWorktrees(m.fs, worktrees, m.PackageManagers, m.RepoPath); err != nil {
		return nil, fmt.Errorf("failed to rebuild registry: %w", err)
	}

	// Scan each worktree for issues
	for branch, worktreePath := range worktrees {

		// Detect package managers
		detectedPMs, err := DetectPackageManagers(m.fs, worktreePath, m.PackageManagers)
		if err != nil {
			continue
		}

		for _, pm := range detectedPMs {
			// Find lockfile
			lockfilePath, err := pm.FindLockfile(m.fs, worktreePath)
			if err != nil {
				continue
			}

			// Compute expected hash
			expectedHash, err := pm.HashLockfile(m.fs, lockfilePath)
			if err != nil {
				continue
			}

			expectedDepsKey := pm.GetDepsKey(expectedHash)
			expectedDepsPath := m.getDepsPath(expectedDepsKey)
			symlinkPath := filepath.Join(worktreePath, pm.DepsDir)

			// Check what's at the symlink path.
			// ReadlinkIfPossible is checked first because afero.Exists follows
			// symlinks and returns false for dangling symlinks, which would
			// incorrectly classify them as IssueMissingDeps.
			isSymlink := false
			var currentTarget string
			if linker, ok := m.fs.(afero.Symlinker); ok {
				target, err := linker.ReadlinkIfPossible(symlinkPath)
				if err == nil && target != "" {
					isSymlink = true
					currentTarget = target
				}
			}

			if !isSymlink {
				// Not a symlink - check if a real path exists
				symlinkExists, err := afero.Exists(m.fs, symlinkPath)
				if err != nil {
					continue
				}

				if !symlinkExists {
					// Missing deps
					issues = append(issues, Issue{
						Type:         IssueMissingDeps,
						WorktreePath: worktreePath,
						Branch:       branch,
						PM:           pm,
						ExpectedHash: expectedHash,
						DepsKey:      expectedDepsKey,
					})
					continue
				}

				// Local folder instead of symlink
				size := m.getDirSize(symlinkPath)
				issues = append(issues, Issue{
					Type:         IssueLocalFolder,
					WorktreePath: worktreePath,
					Branch:       branch,
					PM:           pm,
					ExpectedHash: expectedHash,
					DepsKey:      expectedDepsKey,
					Size:         size,
				})
				continue
			}

			// It's a symlink - check if it points to the right place.
			// Errors from Exists are intentionally ignored here: a failure to stat
			// the target (e.g., permission denied) is treated the same as missing,
			// which is conservative — we'd rather report a broken symlink than
			// silently skip a genuinely inaccessible target.
			targetExists, _ := afero.Exists(m.fs, currentTarget)
			if currentTarget != expectedDepsPath {
				if !targetExists {
					// Broken symlink (points to non-existent target)
					issues = append(issues, Issue{
						Type:          IssueBrokenSymlink,
						WorktreePath:  worktreePath,
						Branch:        branch,
						PM:            pm,
						ExpectedHash:  expectedHash,
						DepsKey:       expectedDepsKey,
						SymlinkTarget: currentTarget,
					})
				} else {
					// Stale symlink (points to old version)
					issues = append(issues, Issue{
						Type:          IssueStaleSymlink,
						WorktreePath:  worktreePath,
						Branch:        branch,
						PM:            pm,
						ExpectedHash:  expectedHash,
						DepsKey:       expectedDepsKey,
						SymlinkTarget: currentTarget,
					})
				}
			} else if !targetExists {
				// Symlink points to the correct key but the target directory is missing
				// (e.g., it was garbage-collected while the symlink still referenced it)
				issues = append(issues, Issue{
					Type:          IssueBrokenSymlink,
					WorktreePath:  worktreePath,
					Branch:        branch,
					PM:            pm,
					ExpectedHash:  expectedHash,
					DepsKey:       expectedDepsKey,
					SymlinkTarget: currentTarget,
				})
			}
		}
	}

	return issues, nil
}

// Fix repairs dependency issues
func (m *DepsManager) Fix(issues []Issue, force bool) error {
	for _, issue := range issues {
		switch issue.Type {
		case IssueLocalFolder:
			// Move local folder to trash and create symlink
			symlinkPath := filepath.Join(issue.WorktreePath, issue.PM.DepsDir)
			if _, err := m.trash.Move(symlinkPath); err != nil {
				return fmt.Errorf("failed to trash local folder: %w", err)
			}

			// Ensure deps exist
			depsPath := m.getDepsPath(issue.DepsKey)
			depsExists, _ := afero.DirExists(m.fs, depsPath)
			if !depsExists {
				lockfilePath, _ := issue.PM.FindLockfile(m.fs, issue.WorktreePath)
				// Resolve install command for this branch
				resolvedPM := ResolveInstallCmd(issue.PM, m.HopspaceConfig, issue.Branch)
				if err := m.installDeps(depsPath, issue.WorktreePath, *resolvedPM); err != nil {
					return fmt.Errorf("failed to install deps: %w", err)
				}
				m.Registry.UpdateEntryMetadata(issue.DepsKey, issue.ExpectedHash, filepath.Base(lockfilePath))
			}

			// Create symlink
			if err := m.createSymlink(depsPath, symlinkPath); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}

			m.Registry.AddUsage(issue.DepsKey, issue.Branch)

		case IssueBrokenSymlink, IssueStaleSymlink:
			// Remove symlink and recreate
			symlinkPath := filepath.Join(issue.WorktreePath, issue.PM.DepsDir)
			if err := m.fs.Remove(symlinkPath); err != nil {
				return fmt.Errorf("failed to remove symlink: %w", err)
			}

			// Ensure deps exist
			depsPath := m.getDepsPath(issue.DepsKey)
			depsExists, _ := afero.DirExists(m.fs, depsPath)
			if !depsExists {
				lockfilePath, _ := issue.PM.FindLockfile(m.fs, issue.WorktreePath)
				// Resolve install command for this branch
				resolvedPM := ResolveInstallCmd(issue.PM, m.HopspaceConfig, issue.Branch)
				if err := m.installDeps(depsPath, issue.WorktreePath, *resolvedPM); err != nil {
					return fmt.Errorf("failed to install deps: %w", err)
				}
				m.Registry.UpdateEntryMetadata(issue.DepsKey, issue.ExpectedHash, filepath.Base(lockfilePath))
			}

			// Create symlink
			if err := m.createSymlink(depsPath, symlinkPath); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}

			m.Registry.AddUsage(issue.DepsKey, issue.Branch)

		case IssueMissingDeps:
			// Install and create symlink
			depsPath := m.getDepsPath(issue.DepsKey)
			symlinkPath := filepath.Join(issue.WorktreePath, issue.PM.DepsDir)

			lockfilePath, _ := issue.PM.FindLockfile(m.fs, issue.WorktreePath)
			// Resolve install command for this branch
			resolvedPM := ResolveInstallCmd(issue.PM, m.HopspaceConfig, issue.Branch)
			if err := m.installDeps(depsPath, issue.WorktreePath, *resolvedPM); err != nil {
				return fmt.Errorf("failed to install deps: %w", err)
			}

			m.Registry.UpdateEntryMetadata(issue.DepsKey, issue.ExpectedHash, filepath.Base(lockfilePath))

			// Create symlink
			if err := m.createSymlink(depsPath, symlinkPath); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}

			m.Registry.AddUsage(issue.DepsKey, issue.Branch)
		}
	}

	// Save registry
	if err := m.Registry.Save(m.fs, m.RepoPath); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	return nil
}

// GarbageCollect removes orphaned dependencies.
// worktrees is a map of branchName → worktreePath.
func (m *DepsManager) GarbageCollect(worktrees map[string]string, dryRun bool) ([]string, int64, error) {
	// Rebuild registry from worktrees
	if err := m.Registry.RebuildFromWorktrees(m.fs, worktrees, m.PackageManagers, m.RepoPath); err != nil {
		return nil, 0, fmt.Errorf("failed to rebuild registry: %w", err)
	}

	// Get orphaned deps
	orphaned := m.Registry.GetOrphaned()
	var totalSize int64

	// Calculate sizes
	for _, depsKey := range orphaned {
		depsPath := m.getDepsPath(depsKey)
		size := m.getDirSize(depsPath)
		totalSize += size
	}

	if dryRun {
		return orphaned, totalSize, nil
	}

	// Delete orphaned deps
	for _, depsKey := range orphaned {
		depsPath := m.getDepsPath(depsKey)
		if err := m.fs.RemoveAll(depsPath); err != nil {
			return orphaned, totalSize, fmt.Errorf("failed to delete %s: %w", depsKey, err)
		}
		m.Registry.DeleteEntry(depsKey)
	}

	// Save registry
	if err := m.Registry.Save(m.fs, m.RepoPath); err != nil {
		return orphaned, totalSize, fmt.Errorf("failed to save registry: %w", err)
	}

	return orphaned, totalSize, nil
}

// getDepsPath returns the full path to a deps directory
func (m *DepsManager) getDepsPath(depsKey string) string {
	dataHome := hop.GetGitHopDataHome()
	// Extract org/repo from repoPath
	// Assuming repoPath is like: /path/to/data-home/org/repo
	if len(m.RepoPath) > len(dataHome) {
		relPath := m.RepoPath[len(dataHome):]
		if len(relPath) > 0 && relPath[0] == filepath.Separator {
			relPath = relPath[1:]
		}
		return filepath.Join(dataHome, relPath, "deps", depsKey)
	}
	// Fallback
	return filepath.Join(m.RepoPath, "deps", depsKey)
}

// getDirSize calculates the total size of a directory
func (m *DepsManager) getDirSize(path string) int64 {
	var size int64
	afero.Walk(m.fs, path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

