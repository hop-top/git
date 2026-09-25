package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// Some installs cannot be shared through the store, and stay in the
// worktree that made them (a local install): pnpm installs
// (LocalReasonPnpm), and any with links out of them. Package managers
// link file:, link: and workspace dependencies with relative links out of
// node_modules. A relative link resolves from the directory it sits in, so
// moved into the store it resolves from the store, not the worktree. Even
// kept absolute it would be wrong: every worktree sharing the install
// would get the installing worktree's workspace packages and sibling
// paths.
//
// Whether an install is one of these is read from the install itself
// (localReasonOf), whatever the package manager.

// LocalInstallMarker names the file git-hop writes into a DepsDir it
// installed in the worktree instead of the store. It holds the hash of
// the lockfile the install was made from.
const LocalInstallMarker = ".git-hop-local"

// LocalReason says why an install cannot be shared through the store.
type LocalReason string

const (
	// LocalReasonLinks: the install has links out of it (file:, link:
	// or workspace dependencies), see hasOutwardLink.
	LocalReasonLinks LocalReason = "links"
	// LocalReasonPnpm: pnpm made the install (pnpmModulesFile). pnpm
	// refuses to install through a link to a directory outside the
	// project (ERR_PNPM_UNSAFE_MODULES_DIR), and already shares packages
	// through its own content-addressable store.
	LocalReasonPnpm LocalReason = "pnpm"
)

// pnpmModulesFile is the file pnpm writes into every node_modules it
// installs.
const pnpmModulesFile = ".modules.yaml"

// String says why an install with this reason cannot be shared.
func (r LocalReason) String() string {
	switch r {
	case LocalReasonLinks:
		return "it has links out of it (file:, link: or workspace dependencies)"
	case LocalReasonPnpm:
		return "pnpm installs into a directory of the worktree only"
	}
	return string(r)
}

// errOutwardLink stops the walk in hasOutwardLink at the first link found.
var errOutwardLink = errors.New("outward link")

// localReasonOf returns why the install at dir cannot be shared, or "" if
// it can. worktree is the worktree the install was made in, or "" for an
// install in the store.
func localReasonOf(fs afero.Fs, dir, worktree string) (LocalReason, error) {
	if isPnpmInstall(fs, dir) {
		return LocalReasonPnpm, nil
	}
	found, err := hasOutwardLink(fs, dir, worktree)
	if err != nil {
		return "", err
	}
	if found {
		return LocalReasonLinks, nil
	}
	return "", nil
}

// isPnpmInstall reports whether pnpm made the install at dir.
func isPnpmInstall(fs afero.Fs, dir string) bool {
	ok, _ := afero.Exists(fs, filepath.Join(dir, pnpmModulesFile))
	return ok
}

// hasOutwardLink reports whether a symlink below dir (directory links are
// not followed) points out of dir by a relative path, or, when worktree is
// given, into worktree by any path. Absolute links elsewhere (a system
// binary, a global cache) resolve the same from anywhere.
func hasOutwardLink(fs afero.Fs, dir, worktree string) (bool, error) {
	reader, ok := fs.(afero.LinkReader)
	if !ok {
		return false, nil
	}
	roots := []string{}
	if worktree != "" {
		roots = append(roots, filepath.Clean(worktree))
		if real, err := filepath.EvalSymlinks(worktree); err == nil && real != worktree {
			roots = append(roots, real)
		}
	}
	err := afero.Walk(fs, dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := reader.ReadlinkIfPossible(path)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			if !isWithin(filepath.Join(filepath.Dir(path), target), dir) {
				return errOutwardLink
			}
			return nil
		}
		for _, root := range roots {
			if isWithin(filepath.Clean(target), root) && !isWithin(filepath.Clean(target), dir) {
				return errOutwardLink
			}
		}
		return nil
	})
	if errors.Is(err, errOutwardLink) {
		return true, nil
	}
	return false, err
}

// readLocalMarker returns the lockfile hash the local install at depsDir
// is marked with, if it is marked.
func readLocalMarker(fs afero.Fs, depsDir string) (string, bool) {
	data, err := afero.ReadFile(fs, filepath.Join(depsDir, LocalInstallMarker))
	if err != nil {
		return "", false
	}
	hash := strings.TrimSpace(string(data))
	return hash, hash != ""
}

// writeLocalMarker marks the install at depsDir as a local install made
// from the lockfile with hash.
func writeLocalMarker(fs afero.Fs, depsDir, hash string) error {
	return afero.WriteFile(fs, filepath.Join(depsDir, LocalInstallMarker), []byte(hash+"\n"), 0o644)
}

// pnpFiles are the files yarn's Plug'n'Play linker writes in the project
// root instead of a node_modules: .pnp.cjs, or .pnp.js before yarn 3.
var pnpFiles = []string{".pnp.cjs", ".pnp.js"}

// usesPnP reports whether the worktree resolves its packages through
// Plug'n'Play, which needs no DepsDir.
func usesPnP(fs afero.Fs, worktreePath string) bool {
	for _, name := range pnpFiles {
		if ok, _ := afero.Exists(fs, filepath.Join(worktreePath, name)); ok {
			return true
		}
	}
	return false
}

// isRealDir reports whether path is a directory and not a link to one.
func isRealDir(fs afero.Fs, path string) bool {
	info, err := lstat(fs, path)
	return err == nil && info.IsDir()
}
