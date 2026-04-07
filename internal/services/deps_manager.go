package services

import (
	"errors"
	"fmt"
	"io"
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

// ensurePMDeps ensures deps for a specific package manager.
//
// Ordering matters: any pre-existing state at worktreePath/<DepsDir> (a
// stale symlink pointing to an OLD cache, or a real directory from a
// pre-sharing install) MUST be cleaned up BEFORE the install command runs.
// Otherwise the install command — which runs with cwd=worktreePath and
// writes into ./<DepsDir> — would either dereference a stale symlink and
// corrupt the OLD shared cache (used by other branches), or write into a
// stale real directory that's about to be relocated. See
// https://github.com/hop-top/git/pull/13 review.
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

	// Fast path: existing correct symlink to a populated cache → nothing to
	// do. This short-circuits before touching the worktree or running the
	// install command.
	if existing, ok := readSymlink(m.fs, symlinkPath); ok && existing == depsPath {
		if exists, _ := afero.DirExists(m.fs, depsPath); exists {
			m.Registry.AddUsage(depsKey, branch)
			return nil
		}
	}

	// Clean up any pre-existing state at symlinkPath so the install command
	// runs against a fresh path. Symlinks (stale or otherwise) must be
	// removed, not dereferenced, to avoid writing into another branch's
	// cache. Real directories are moved to trash (recoverable via git hop
	// doctor). This must happen BEFORE installDeps.
	if err := m.cleanWorktreeDepsPath(symlinkPath); err != nil {
		return err
	}

	// Check if the target cache already exists (populated by a sibling
	// branch with the same lockfile hash). If not, install.
	depsExists, err := afero.DirExists(m.fs, depsPath)
	if err != nil {
		return fmt.Errorf("failed to check deps existence: %w", err)
	}

	if !depsExists {
		if err := m.installDeps(depsPath, worktreePath, *resolvedPM); err != nil {
			if errors.Is(err, ErrBinaryNotFound) {
				// Binary not available in this environment; skip silently.
				return nil
			}
			return fmt.Errorf("failed to install deps: %w", err)
		}
		m.Registry.UpdateEntryMetadata(depsKey, hash, filepath.Base(lockfilePath))
	}

	// Create symlink
	if err := m.createSymlink(depsPath, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	// Add usage to registry
	m.Registry.AddUsage(depsKey, branch)

	return nil
}

// readSymlink returns the target of a symlink at path, or ("", false) if
// path is not a symlink or cannot be read.
func readSymlink(fs afero.Fs, path string) (string, bool) {
	linker, ok := fs.(afero.Symlinker)
	if !ok {
		return "", false
	}
	target, err := linker.ReadlinkIfPossible(path)
	if err != nil {
		return "", false
	}
	return target, true
}

// cleanWorktreeDepsPath removes any pre-existing state at symlinkPath so
// the install command runs against a fresh path. Symlinks are removed
// (never dereferenced) to avoid corrupting whatever they point to. Real
// directories are moved to trash for recovery.
func (m *DepsManager) cleanWorktreeDepsPath(symlinkPath string) error {
	if _, isSymlink := readSymlink(m.fs, symlinkPath); isSymlink {
		if err := m.fs.Remove(symlinkPath); err != nil {
			return fmt.Errorf("failed to remove stale symlink: %w", err)
		}
		return nil
	}
	exists, err := afero.Exists(m.fs, symlinkPath)
	if err != nil {
		return fmt.Errorf("failed to check worktree deps path: %w", err)
	}
	if exists {
		if _, err := m.trash.Move(symlinkPath); err != nil {
			return fmt.Errorf("failed to trash local deps: %w", err)
		}
	}
	return nil
}

// RelocateDir moves src to dst. It first tries an atomic os.Rename. If
// that fails for any reason (cross-filesystem EXDEV, dst exists as the
// wrong type, etc.), it falls back to a recursive copy + RemoveAll of src.
// The fallback mirrors the pattern trash.go uses for the same class of
// failure. dst is removed before the rename/copy so both paths start from a
// clean slate.
func RelocateDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("failed to clear destination %s: %w", dst, err)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Rename failed (EXDEV, ENOTDIR, etc.). Fall back to copy + remove.
	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", src, dst, err)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("failed to remove source %s after copy: %w", src, err)
	}
	return nil
}

// copyTree recursively copies a directory tree from src to dst. It preserves
// file modes but not ownership or extended attributes (sufficient for
// package-manager output which is regenerable from lockfiles anyway).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single regular file preserving mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// installDeps installs dependencies to the shared cache at targetDir.
//
// Package managers fall into two camps:
//
//  1. cwd-relative writers — npm ci, pnpm install, go mod vendor, composer
//     install, bundle install: they ignore any target-dir argument and
//     write to ./<DepsDir> of cwd. For these, we run with cwd=worktreePath
//     and then relocate worktreePath/<DepsDir> into targetDir.
//  2. target-dir writers — pip: `python -m venv <targetDir>` populates
//     targetDir directly, so no worktree-local DepsDir is produced.
//
// After install, exactly one of the two must be populated:
//   - worktreePath/<DepsDir> exists → relocate it into targetDir.
//   - targetDir has content (from a target-dir writer) → nothing to do.
//   - neither → install silently produced nothing; error out rather than
//     leave an empty cache entry behind.
//
// See https://github.com/hop-top/git/issues/11 and PR #13 review.
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

	// Case 1: install wrote into worktreePath/<DepsDir>. Relocate it into
	// the shared cache. ensurePMDeps guarantees this path was clean before
	// the install ran, so anything here now was produced by the install
	// command (not a stale symlink/dir).
	worktreeDeps := filepath.Join(worktreePath, pm.DepsDir)
	if exists, err := afero.DirExists(m.fs, worktreeDeps); err != nil {
		return fmt.Errorf("failed to check worktree deps dir: %w", err)
	} else if exists {
		if err := RelocateDir(worktreeDeps, targetDir); err != nil {
			m.fs.RemoveAll(targetDir)
			return fmt.Errorf("failed to relocate %s into shared cache: %w", pm.DepsDir, err)
		}
		return nil
	}

	// Case 2: target-dir writer (pip). Verify the install actually put
	// something into targetDir. An empty targetDir after install means the
	// command silently produced nothing — surface the bug instead of
	// caching the emptiness.
	populated, err := dirHasEntries(targetDir)
	if err != nil {
		return fmt.Errorf("failed to inspect target directory: %w", err)
	}
	if !populated {
		m.fs.RemoveAll(targetDir)
		return fmt.Errorf("install for package manager %q produced no deps (neither %s/%s nor %s was populated)", pm.Name, worktreePath, pm.DepsDir, targetDir)
	}
	return nil
}

// dirHasEntries reports whether path is a directory containing at least
// one entry. Returns false if path does not exist.
func dirHasEntries(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return len(entries) > 0, nil
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

