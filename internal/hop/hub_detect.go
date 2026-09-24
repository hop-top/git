package hop

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
)

// repoProbe is what git reports about the repository it discovers from a
// directory. Git resolves core.bare itself, whatever its spelling and
// wherever it is set (an include, the main worktree's config.worktree),
// so nothing here parses a config file.
type repoProbe struct {
	bare      bool
	gitDir    string // absolute
	commonDir string // absolute
}

// probeRepo asks git, in one call, whether the repository discovered from
// dir is bare and where its git dir and common dir are. ok is false when
// git finds no repository or its answer is unreadable.
func probeRepo(g git.GitInterface, dir string) (repoProbe, bool) {
	out, err := g.RevParse(dir, "--is-bare-repository", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return repoProbe{}, false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[1] == "" || lines[2] == "" {
		return repoProbe{}, false
	}
	common := strings.TrimSpace(lines[2])
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return repoProbe{
		bare:      strings.TrimSpace(lines[0]) == "true",
		gitDir:    strings.TrimSpace(lines[1]),
		commonDir: common,
	}, true
}

// linked reports whether the probed directory is a linked worktree: its
// git dir is a per-worktree admin dir, not the repository's common dir.
func (p repoProbe) linked() bool {
	return !samePath(p.gitDir, p.commonDir)
}

// RepoRootOfWorktree returns the root of the repository the linked
// worktree at dir belongs to, from git's common dir: the common dir itself
// for a bare hub, the directory holding it when it is a .git directory.
// ok is false when git does not report dir as a linked worktree.
func RepoRootOfWorktree(g git.GitInterface, dir string) (string, bool) {
	p, ok := probeRepo(g, dir)
	if !ok || !p.linked() {
		return "", false
	}
	root := filepath.Clean(p.commonDir)
	if filepath.Base(root) == ".git" {
		root = filepath.Dir(root)
	}
	return root, true
}

// isBareRepoRoot reports whether dir is itself the git dir of a bare
// repository. The structural check runs first and spawns nothing, so a
// caller walking up a tree only asks git about directories that look
// like one.
func isBareRepoRoot(fs afero.Fs, g git.GitInterface, dir string) bool {
	if !isBareRepoAtPath(fs, dir) {
		return false
	}
	p, ok := probeRepo(g, dir)
	return ok && p.bare && samePath(p.gitDir, dir)
}

// FindUnregisteredHub walks upward from startPath for a bare git-hop hub
// that is missing hop.json, and returns the nearest one. A directory
// qualifies when:
//
//  1. it has no hop.json (with one it is a registered hub);
//  2. it has HEAD, objects/ and refs/ directly under it and a non-empty
//     hops/ directory, checked without running git;
//  3. git reports it as a bare repository whose git dir is the
//     directory itself;
//  4. git lists at least one of its worktrees under hops/.
func FindUnregisteredHub(fs afero.Fs, g git.GitInterface, startPath string) (string, bool) {
	current, err := filepath.Abs(startPath)
	if err != nil {
		return "", false
	}
	for {
		if isUnregisteredHub(fs, g, current) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func isUnregisteredHub(fs afero.Fs, g git.GitInterface, dir string) bool {
	if IsHub(fs, dir) {
		return false
	}
	hopsDir := filepath.Join(dir, "hops")
	if entries, err := afero.ReadDir(fs, hopsDir); err != nil || len(entries) == 0 {
		return false
	}
	if !isBareRepoRoot(fs, g, dir) {
		return false
	}
	return hasWorktreeUnder(g, dir, hopsDir)
}

// hasWorktreeUnder reports whether git lists a worktree of repo inside
// root. A worktree whose directory is gone still counts: git keeps it
// registered until it is pruned.
func hasWorktreeUnder(g git.GitInterface, repo, root string) bool {
	out, err := g.WorktreeListPorcelain(repo)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		path, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		if isStrictlyUnder(path, root) {
			return true
		}
	}
	return false
}

// resolvedPath resolves symlinks when the path exists (macOS /tmp is
// /private/tmp; git records resolved paths) and cleans it otherwise.
func resolvedPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func samePath(a, b string) bool {
	return resolvedPath(a) == resolvedPath(b)
}

// isStrictlyUnder reports whether path lies below root, comparing both
// as given and with symlinks resolved.
func isStrictlyUnder(path, root string) bool {
	below := func(p, r string) bool {
		rel, err := filepath.Rel(r, p)
		return err == nil && rel != "." && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return below(filepath.Clean(path), filepath.Clean(root)) ||
		below(resolvedPath(path), resolvedPath(root))
}
