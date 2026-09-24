package state

import (
	"path/filepath"
	"strings"
)

// WorktreeKey is the key a worktree at path is recorded under in
// RepositoryState.Worktrees: the path made absolute and cleaned. Symlinks
// are not resolved, as the path was given; lookups compare with SamePath,
// so another spelling of the same directory finds the entry.
func WorktreeKey(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// SamePath reports whether a and b name the same path once symlinks in
// their longest existing prefix are resolved (ResolvePath). On macOS the
// temp dir under /var is really under /private/var, and git records the
// resolved spelling while hop.json may not.
func SamePath(a, b string) bool {
	return ResolvePath(a) == ResolvePath(b)
}

// ResolvePath makes path absolute and resolves symlinks in its longest
// existing prefix; the part that does not exist is appended unchanged,
// since a worktree state records may be gone.
func ResolvePath(path string) string {
	if path == "" {
		return ""
	}
	abs := WorktreeKey(path)
	rest := ""
	for cur := abs; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// pathWithin reports whether path is dir or lies below it, compared
// resolved.
func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(ResolvePath(dir), ResolvePath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
