package services

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
)

// DepsManager manages shared dependencies across worktrees
type DepsManager struct {
	Registry        *DepsRegistry
	RepoPath        string
	PackageManagers []PackageManager
	HopspaceConfig  *config.HopspaceConfig // For resolving install command overrides
	fs              afero.Fs
	trash           *Trash
	linkScope       []string // see SetLinkScope
	linkScopeSet    bool
}

// IssueType represents the type of dependency issue
type IssueType string

const (
	IssueLocalFolder   IssueType = "local_folder"
	IssueBrokenSymlink IssueType = "broken_symlink"
	IssueStaleSymlink  IssueType = "stale_symlink"
	IssueMissingDeps   IssueType = "missing_deps"
	// IssueOldLayout marks a link to an install for the current lockfile
	// in the layout earlier releases wrote (flatDepsKey), where Node
	// cannot resolve the install's packages from one another.
	IssueOldLayout IssueType = "old_layout"
	// IssueDamagedInstall marks a link to a store install that is missing
	// entries it was installed with: emptied through a worktree's link
	// (npm ci, rm -rf node_modules/*), which breaks every worktree
	// linked to it.
	IssueDamagedInstall IssueType = "damaged_install"
	// IssueStaleLocal marks a local install (LocalInstallMarker) made
	// from an older lockfile, or not marked but unshareable anyway. The
	// next install refreshes it in place.
	IssueStaleLocal IssueType = "stale_local"
	// IssueNeedsLocal marks a link to a store install that must not be
	// shared (LocalReason), made before such installs were kept local.
	IssueNeedsLocal IssueType = "needs_local"
)

// Severity classifies how much attention an Issue deserves.
type Severity string

const (
	// SeverityError marks a broken state that will not resolve itself and
	// needs repair (missing or dangling deps, an unshared local folder).
	SeverityError Severity = "error"
	// SeverityWarning marks a benign, self-healing state: the deps are
	// present and usable, they simply predate the current lockfile. The
	// next install refreshes them. Reported, but never on its own a reason
	// for doctor to declare the installation unhealthy.
	SeverityWarning Severity = "warning"
)

// Severity returns the severity of an issue type. Stale symlinks are
// warnings: a worktree that has not re-run its installer since the
// lockfile changed still has working deps, so calling it an error
// overstated the problem and made `doctor` look broken on healthy repos.
// Old-layout links are warnings too: the next install relinks them. So
// are local installs made from an older lockfile: the next install
// refreshes them in place.
func (t IssueType) Severity() Severity {
	if t == IssueStaleSymlink || t == IssueOldLayout || t == IssueStaleLocal {
		return SeverityWarning
	}
	return SeverityError
}

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
	// LocalReason says why the install must stay in the worktree
	// (IssueNeedsLocal, and IssueStaleLocal for an unmarked install).
	LocalReason LocalReason
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

// DetectInWorktree returns the package managers detected in the given
// worktree. Callers use this to decide whether to surface
// "Setting up dependencies…" UX before invoking EnsureDeps — emitting
// that message unconditionally produces misleading output for projects
// with no detectable package manager. Covered by
// TestAdd_NoDockerProject_NoEnvNoise.
func (m *DepsManager) DetectInWorktree(worktreePath string) ([]PackageManager, error) {
	return DetectPackageManagers(m.fs, worktreePath, m.PackageManagers)
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

	// Go projects bypass the cache+symlink pipeline entirely. The pipeline
	// assumes its DepsDir (here: vendor/) is a regenerable cache living
	// outside source control, but Go vendor/ is fundamentally different:
	// it is committed to git when used, lives in the worktree, and is
	// materialised by `git checkout`. Running the pipeline against
	// vendor/ would:
	//   - trash a user's committed vendor/ in cleanWorktreeDepsPath, and
	//   - auto-create an unwanted vendor/ via `go mod vendor` even when
	//     the source tree has none (regression in
	//     TestAdd_GoProject_NoVendorWhenNotVendored).
	//
	// We also never auto-invoke `go mod vendor`: with a mismatched go.mod
	// (e.g. no requires) it removes a user's committed vendor/
	// (regression in TestAdd_GoProject_VendorPreservedWhenVendored).
	// vendor/ is the user's source of truth — git-hop must not mutate it.
	// `go mod download` is the only side-effect-free warm-up we need; it
	// populates GOMODCACHE without touching the worktree.
	if pm.Name == "go" {
		if _, err := exec.LookPath("go"); err != nil {
			return nil
		}
		cmd := exec.Command("go", "mod", "download")
		cmd.Dir = worktreePath
		_ = cmd.Run() // best effort; module cache is regenerable
		return nil
	}

	// Resolve install command with hierarchy (branch > repo > global)
	resolvedPM := ResolveInstallCmd(pm, m.HopspaceConfig, branch)

	hash, err := resolvedPM.HashLockfile(m.fs, lockfilePath)
	if err != nil {
		return fmt.Errorf("failed to hash lockfile: %w", err)
	}

	err = m.linkDeps(worktreePath, branch, *resolvedPM, hash, lockfilePath)
	if errors.Is(err, ErrBinaryNotFound) {
		// Binary not available in this environment; skip silently.
		// linkDeps has put back any link it took down.
		return nil
	}
	return err
}

// readSymlink returns the target of a symlink at path, or ("", false) if
// path is not a symlink or cannot be read. An empty target is treated as
// "not a symlink" to stay consistent with Audit's symlink detection, which
// rejects empty targets to avoid ambiguous classification.
func readSymlink(fs afero.Fs, path string) (string, bool) {
	linker, ok := fs.(afero.Symlinker)
	if !ok {
		return "", false
	}
	target, err := linker.ReadlinkIfPossible(path)
	if err != nil || target == "" {
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

// relocateRename is the rename function used by RelocateDir. It is a
// package-level var so tests can inject a failing rename to force the
// copy-fallback path without needing a real cross-filesystem setup.
// Production code never reassigns it; only SetRelocateRenameForTest does.
var relocateRename = os.Rename

// SetRelocateRenameForTest replaces the rename function used by
// RelocateDir for the duration of a test. It returns a restore function
// that must be called (typically via defer) to reset the default. Not for
// production use.
func SetRelocateRenameForTest(fn func(string, string) error) func() {
	prev := relocateRename
	relocateRename = fn
	return func() { relocateRename = prev }
}

// RelocateDir moves src to dst. When fs is *afero.OsFs it first tries an
// atomic os.Rename; if that fails for any reason (cross-filesystem EXDEV,
// dst exists as the wrong type, etc.) it falls back to a recursive copy
// plus RemoveAll of src. For any other afero backend it always uses the
// afero-based copy path. dst is removed before the rename/copy so both
// paths start from a clean slate. Mirrors the pattern in trash.go.
func RelocateDir(fs afero.Fs, src, dst string) error {
	if err := fs.RemoveAll(dst); err != nil {
		return fmt.Errorf("failed to clear destination %s: %w", dst, err)
	}
	if _, ok := fs.(*afero.OsFs); ok {
		if err := relocateRename(src, dst); err == nil {
			return nil
		}
		// Rename failed (EXDEV, ENOTDIR, injected test error, etc.).
		// Fall through to copy + remove.
	}
	if err := copyTree(fs, src, dst); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", src, dst, err)
	}
	if err := fs.RemoveAll(src); err != nil {
		return fmt.Errorf("failed to remove source %s after copy: %w", src, err)
	}
	return nil
}

// copyTree recursively copies a directory tree from src to dst via the
// given afero filesystem. It preserves file modes but not ownership or
// extended attributes (sufficient for package-manager output which is
// regenerable from lockfiles anyway). Symlinks are preserved when the
// filesystem supports afero.Symlinker.
func copyTree(fs afero.Fs, src, dst string) error {
	return afero.Walk(fs, src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.Mode()&os.ModeSymlink != 0 {
			linker, ok := fs.(afero.Symlinker)
			if !ok {
				// Filesystem can't read/write symlinks; skip silently so
				// tree walks on non-symlink backends still succeed.
				return nil
			}
			link, err := linker.ReadlinkIfPossible(path)
			if err != nil {
				return err
			}
			return linker.SymlinkIfPossible(link, target)
		}
		if info.IsDir() {
			return fs.MkdirAll(target, info.Mode())
		}
		return copyFile(fs, path, target, info.Mode())
	})
}

// copyFile copies a single regular file preserving mode. It returns any
// error from Close so callers see write failures that only surface when
// buffered writes are flushed (e.g. on some network filesystems).
func copyFile(fs afero.Fs, src, dst string, mode os.FileMode) error {
	in, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := fs.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// installDeps runs pm's install for worktreePath and puts the result in
// the store at depsKey, or, when it cannot be shared, leaves it in the
// worktree as a local install marked with hash. It reports whether the
// install stayed local.
//
// Package managers fall into two camps:
//
//  1. cwd-relative writers — npm ci, pnpm install, go mod vendor, composer
//     install, bundle install: they ignore any target-dir argument and
//     write to ./<DepsDir> of cwd. For these, we run with cwd=worktreePath
//     and then relocate worktreePath/<DepsDir> into the store, unless it
//     has links out of it (localReasonOf).
//  2. target-dir writers — pip: `python -m venv <targetDir>` populates
//     the store's directory directly, so no worktree-local DepsDir is
//     produced.
//
// After install, exactly one of the two must be populated:
//   - worktreePath/<DepsDir> exists → keep it local or relocate it.
//   - the store's directory has content (from a target-dir writer) →
//     nothing to do.
//   - neither, but the worktree resolves through Plug'n'Play (usesPnP) →
//     a local install with no DepsDir.
//   - neither → install silently produced nothing; error out rather than
//     leave an empty cache entry behind.
//
// A store directory this call did not create (an install found damaged
// or unshareable) is left alone unless the install replaces it: other
// worktrees may still link to it.
//
// See https://github.com/hop-top/git/issues/11 and PR #13 review.
func (m *DepsManager) installDeps(depsKey, worktreePath string, pm PackageManager, hash string) (bool, error) {
	targetDir := m.getDepsPath(depsKey)
	existed, err := afero.Exists(m.fs, targetDir)
	if err != nil {
		return false, fmt.Errorf("failed to check target directory: %w", err)
	}
	dropCreated := func() {
		if !existed {
			_ = m.removeInstall(depsKey)
		}
	}
	if err := m.fs.MkdirAll(targetDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create target directory: %w", err)
	}

	if err := pm.Install(targetDir, worktreePath); err != nil {
		dropCreated()
		return false, fmt.Errorf("failed to run install: %w", err)
	}

	// Case 1: install wrote into worktreePath/<DepsDir>. linkDeps
	// guarantees this path held nothing but a local install before the
	// install ran, so anything here now was produced by the install
	// command (not a stale symlink/dir).
	worktreeDeps := filepath.Join(worktreePath, pm.DepsDir)
	if exists, err := afero.DirExists(m.fs, worktreeDeps); err != nil {
		return false, fmt.Errorf("failed to check worktree deps dir: %w", err)
	} else if exists {
		reason, err := localReasonOf(m.fs, worktreeDeps, worktreePath)
		if err != nil {
			dropCreated()
			return false, fmt.Errorf("failed to check %s for links: %w", worktreeDeps, err)
		}
		if reason != "" {
			dropCreated()
			if err := writeLocalMarker(m.fs, worktreeDeps, hash); err != nil {
				return false, fmt.Errorf("failed to mark local install %s: %w", worktreeDeps, err)
			}
			return true, nil
		}
		// A local install refreshed in place may now be shareable; its
		// marker must not travel into the store.
		if err := m.fs.Remove(filepath.Join(worktreeDeps, LocalInstallMarker)); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("failed to unmark %s: %w", worktreeDeps, err)
		}
		if err := RelocateDir(m.fs, worktreeDeps, targetDir); err != nil {
			m.fs.RemoveAll(targetDir)
			return false, fmt.Errorf("failed to relocate %s into shared cache: %w", pm.DepsDir, err)
		}
		return false, nil
	}

	// Case 2: target-dir writer (pip). Verify the install actually put
	// something into targetDir. An empty targetDir after install means the
	// command silently produced nothing — surface the bug instead of
	// caching the emptiness.
	populated, err := dirHasEntries(m.fs, targetDir)
	if err != nil {
		return false, fmt.Errorf("failed to inspect target directory: %w", err)
	}
	if !populated {
		m.fs.RemoveAll(targetDir)
		dropCreated()
		// Case 3: Plug'n'Play (yarn's default linker) resolves packages
		// through .pnp.cjs in the worktree and writes no DepsDir at all.
		// The install stays in the worktree; there is nothing to share.
		if usesPnP(m.fs, worktreePath) {
			return true, nil
		}
		return false, fmt.Errorf("install for package manager %q produced no deps (neither %s/%s nor %s was populated)", pm.Name, worktreePath, pm.DepsDir, targetDir)
	}
	return false, nil
}

// dirHasEntries reports whether path is a directory containing at least
// one entry. Returns false if path does not exist.
func dirHasEntries(fs afero.Fs, path string) (bool, error) {
	entries, err := afero.ReadDir(fs, path)
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

// skipGoVendor reports whether the Go package manager's vendor/ directory
// at worktreePath is outside git-hop's remit and must therefore be left
// alone by audit and repair. The rule is IsGoVendorActive, shared with the
// worktree-create install path so the two can never drift apart. Only the
// built-in "go" package manager is affected: other managers whose DepsDir
// happens to be vendor/ (composer, bundler) treat it as a regenerable
// cache and are audited normally.
func skipGoVendor(fs afero.Fs, pm PackageManager, worktreePath string) bool {
	if pm.Name != "go" || pm.DepsDir != "vendor" {
		return false
	}
	return !IsGoVendorActive(fs, worktreePath)
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
		detectedPMs, err := DetectPackageManagers(m.fs, worktreePath, m.PackageManagers)
		if err != nil {
			continue
		}
		for _, pm := range detectedPMs {
			if issue, ok := m.auditDeps(branch, worktreePath, pm); ok {
				issues = append(issues, issue)
			}
		}
	}
	return issues, nil
}

// Fix repairs dependency issues
func (m *DepsManager) Fix(issues []Issue, force bool) error {
	for _, issue := range issues {
		// Defence in depth: Audit already filters these out, but Fix is
		// exported and may be handed an issue list assembled elsewhere or
		// by an older audit. Creating vendor/ in a repo that gitignores it
		// is the exact harm this fix exists to prevent, so re-check here
		// rather than trust the caller.
		if skipGoVendor(m.fs, issue.PM, issue.WorktreePath) {
			continue
		}

		switch issue.Type {
		case IssueLocalFolder, IssueBrokenSymlink, IssueStaleSymlink, IssueOldLayout, IssueMissingDeps, IssueDamagedInstall, IssueStaleLocal, IssueNeedsLocal:
		default:
			continue
		}
		// linkDeps moves a local folder to trash and takes down a link
		// only to replace it, putting the link back if the install fails.
		lockfilePath, _ := issue.PM.FindLockfile(m.fs, issue.WorktreePath)
		resolvedPM := ResolveInstallCmd(issue.PM, m.HopspaceConfig, issue.Branch)
		if err := m.linkDeps(issue.WorktreePath, issue.Branch, *resolvedPM, issue.ExpectedHash, lockfilePath); err != nil {
			return err
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
	orphaned, err := m.orphaned(worktrees)
	if err != nil {
		return nil, 0, err
	}
	var totalSize int64

	// Calculate sizes
	for _, depsKey := range orphaned {
		totalSize += m.getDirSize(m.getDepsPath(depsKey))
	}

	if dryRun {
		return orphaned, totalSize, nil
	}

	// Delete orphaned deps
	for _, depsKey := range orphaned {
		if err := m.removeInstall(depsKey); err != nil {
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

// getDepsPath returns where depsKey is installed in this hopspace's store.
func (m *DepsManager) getDepsPath(depsKey string) string {
	return getDepsPath(m.RepoPath, depsKey)
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
