package services

import (
	"path/filepath"
	"slices"

	"github.com/spf13/afero"
)

// auditDeps classifies the DepsDir of pm in worktreePath, returning the
// issue it has, if any.
func (m *DepsManager) auditDeps(branch, worktreePath string, pm PackageManager) (Issue, bool) {
	// Go vendor/ is only ours to audit when vendor mode is active.
	// Skipping here mirrors the create path (see ensurePMDeps and
	// IsGoVendorActive); without it doctor reported "missing vendor" for
	// every Go repo that gitignores vendor/, and --fix materialised the
	// unwanted directory.
	if skipGoVendor(m.fs, pm, worktreePath) {
		return Issue{}, false
	}
	lockfilePath, err := pm.FindLockfile(m.fs, worktreePath)
	if err != nil {
		return Issue{}, false
	}
	expectedHash, err := pm.HashLockfile(m.fs, lockfilePath)
	if err != nil {
		return Issue{}, false
	}

	expectedDepsKey := pm.GetDepsKey(expectedHash)
	expectedDepsPath := m.getDepsPath(expectedDepsKey)
	symlinkPath := filepath.Join(worktreePath, pm.DepsDir)
	issue := Issue{
		WorktreePath: worktreePath,
		Branch:       branch,
		PM:           pm,
		ExpectedHash: expectedHash,
		DepsKey:      expectedDepsKey,
	}

	// A link is read first: afero.Exists follows links and reports a
	// dangling one missing, which would misclassify it as IssueMissingDeps.
	currentTarget, isSymlink := readSymlink(m.fs, symlinkPath)
	if !isSymlink {
		exists, err := afero.Exists(m.fs, symlinkPath)
		if err != nil {
			return Issue{}, false
		}
		if !exists {
			// Plug'n'Play installs write no DepsDir (usesPnP).
			if usesPnP(m.fs, worktreePath) {
				return Issue{}, false
			}
			issue.Type = IssueMissingDeps
			return issue, true
		}
		if _, marked := readLocalMarker(m.fs, symlinkPath); !marked {
			if install, ok := m.entryLinksInstall(symlinkPath, pm); ok {
				return m.auditEntryLinks(issue, symlinkPath, install)
			}
		}
		return m.auditLocalDeps(issue, symlinkPath)
	}

	// Errors from Exists are intentionally ignored here: a failure to stat
	// the target (e.g., permission denied) is treated the same as missing,
	// which is conservative — we'd rather report a broken symlink than
	// silently skip a genuinely inaccessible target. A link to the right
	// install whose directory is gone (e.g. collected while the link still
	// referenced it) is broken too.
	issue.SymlinkTarget = currentTarget
	targetExists, _ := afero.Exists(m.fs, currentTarget)
	switch {
	case !targetExists:
		issue.Type = IssueBrokenSymlink
	case m.damagedStoreInstall(pm, currentTarget):
		issue.Type = IssueDamagedInstall
	case m.unshareableStoreInstall(pm, currentTarget, &issue):
		issue.Type = IssueNeedsLocal
	case currentTarget == expectedDepsPath:
		return Issue{}, false
	case slices.Contains(m.flatDepsPaths(pm, expectedHash), currentTarget):
		issue.Type = IssueOldLayout
	default:
		// Points to an install for an older lockfile.
		issue.Type = IssueStaleSymlink
	}
	return issue, true
}

// auditLocalDeps classifies a DepsDir that is a real directory in the
// worktree. A local install marked for the current lockfile is set up; one
// marked for another, or unmarked but unshareable anyway, is refreshed by
// the next install; anything else should be a link into the store.
func (m *DepsManager) auditLocalDeps(issue Issue, depsDir string) (Issue, bool) {
	if hash, ok := readLocalMarker(m.fs, depsDir); ok {
		if hash == issue.ExpectedHash {
			return Issue{}, false
		}
		issue.Type = IssueStaleLocal
		issue.CurrentHash = hash
		return issue, true
	}
	if reason, err := localReasonOf(m.fs, depsDir, issue.WorktreePath); err == nil && reason != "" {
		issue.Type = IssueStaleLocal
		issue.LocalReason = reason
		return issue, true
	}
	issue.Type = IssueLocalFolder
	issue.Size = m.getDirSize(depsDir)
	return issue, true
}

// unshareableStoreInstall reports whether target, a worktree link's
// target, is one of pm's installs in this store in the current layout that
// must not be shared (storeLocalReason), recording why in issue.
func (m *DepsManager) unshareableStoreInstall(pm PackageManager, target string, issue *Issue) bool {
	key, ok := depsKeyOf(m.RepoPath, target, pm)
	if !ok || isFlatDepsKey(key) {
		return false
	}
	reason, err := m.storeLocalReason(target)
	if err != nil || reason == "" {
		return false
	}
	issue.LocalReason = reason
	return true
}

// storeLocalReason returns why the store install at depsPath must not be
// shared, or "". An install git-hop recorded (installManifest) was checked
// before it was put in the store; only installs an earlier release put
// there are walked for links.
func (m *DepsManager) storeLocalReason(depsPath string) (LocalReason, error) {
	if isPnpmInstall(m.fs, depsPath) {
		return LocalReasonPnpm, nil
	}
	if ok, _ := afero.Exists(m.fs, InstallManifestPath(depsPath)); ok {
		return "", nil
	}
	return localReasonOf(m.fs, depsPath, "")
}
