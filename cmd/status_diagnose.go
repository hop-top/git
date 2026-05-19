package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// unregisteredBareWorktreeHint composes the message printed when status
// detects a bare-worktree-shaped repo missing hop.json. The shape is
// modeled on git's own "you need to do X" notices: a short factual line
// followed by a lowercase "hint:" line carrying the recovery suggestion.
func unregisteredBareWorktreeHint(repoRoot string) string {
	return fmt.Sprintf(
		"Detected a bare-worktree repository at %s, but it is missing hop.json.\n"+
			"hint: run 'git hop init' at %s to register it as a hub.",
		repoRoot, repoRoot,
	)
}

// detectUnregisteredBareWorktreeRepo walks upward from startPath looking
// for a bare-worktree-shaped repo that is missing hop.json. Returns the
// repo root and true on hit; ("", false) otherwise.
//
// A directory matches when all hold:
//
//  1. It contains a git bare-repo config (a "config" file with a line
//     "bare = true" in the [core] section).
//  2. It contains a "hops/" subdirectory with at least one entry (a
//     worktree). An empty hops/ does not count — there's nothing to
//     register.
//  3. It does NOT contain "hop.json" — the marker that would make it a
//     hub already.
//
// The walk stops at the filesystem root. Returns the first match while
// walking upward, which is the nearest enclosing candidate.
func detectUnregisteredBareWorktreeRepo(fs afero.Fs, startPath string) (string, bool) {
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return "", false
	}

	current := absPath
	for {
		if isUnregisteredBareWorktreeRoot(fs, current) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// isUnregisteredBareWorktreeRoot reports whether path is a bare-worktree
// repo root missing hop.json. See detectUnregisteredBareWorktreeRepo for
// criteria.
func isUnregisteredBareWorktreeRoot(fs afero.Fs, path string) bool {
	// hop.json present → already a registered hub, not our case.
	if exists, _ := afero.Exists(fs, filepath.Join(path, "hop.json")); exists {
		return false
	}

	// Must be a bare git repo: <path>/config with "bare = true".
	if !isBareRepoConfig(fs, filepath.Join(path, "config")) {
		return false
	}

	// Must have a non-empty hops/ subdirectory.
	hopsDir := filepath.Join(path, "hops")
	entries, err := afero.ReadDir(fs, hopsDir)
	if err != nil || len(entries) == 0 {
		return false
	}
	return true
}

// isBareRepoConfig parses a git config file and returns true when the
// [core] section contains "bare = true". Only the literal token is
// checked; this is a heuristic for "looks like a bare repo," not a full
// git config parser. Missing file or any read/parse error → false.
func isBareRepoConfig(fs afero.Fs, configPath string) bool {
	data, err := afero.ReadFile(fs, configPath)
	if err != nil {
		return false
	}
	// Cheap scan: any line that, after trimming, matches "bare = true"
	// is sufficient. Git itself writes this exact form for bare repos
	// (see usp/config, git/config in real bare-worktree layouts).
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "bare = true" {
			return true
		}
	}
	return false
}
