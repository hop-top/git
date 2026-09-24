package state

import (
	"sort"
	"strconv"
	"strings"
)

// Releases before 2.0 keyed a repository's worktrees by branch, so two
// hubs of one repository could not both record a worktree of the same
// branch. migrate rekeys such entries by path.
//
// An entry is legacy when its branch field is empty, whatever the file's
// version says: a release that predates the field loads a 2.x file,
// ignores the field in the entries it adds, keeps "version": "2.0.0" and
// saves. So legacy entries are found one by one, not by the version.

// legacyKeyPrefix keys an entry that has no path, which cannot be keyed
// by path. It is kept, never dropped; doctor and prune treat it as a
// worktree that is gone.
const legacyKeyPrefix = "legacy:"

// hasLegacyEntries reports whether st holds an entry of the branch-keyed
// format.
func hasLegacyEntries(st *State) bool {
	for _, repo := range st.Repositories {
		if repo == nil {
			continue
		}
		for _, wt := range repo.Worktrees {
			if wt != nil && wt.Branch == "" {
				return true
			}
		}
	}
	return false
}

// newerThanSupported reports whether version's major number is above the
// format this release writes.
func newerThanSupported(version string) bool {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return false
	}
	current, _, _ := strings.Cut(Version, ".")
	m, _ := strconv.Atoi(current)
	return n > m
}

// migrate rekeys every repository's worktrees by path and reports whether
// anything changed. It never drops an entry that records something:
//
//   - a legacy entry takes its old key as its branch, and the deepest
//     recorded hub containing it as its hub when it names none;
//   - an entry without a path is kept under legacyKeyPrefix+branch;
//   - two entries for the same worktree (the same path, however spelled)
//     become one (mergeEntries).
//
// Migrating a migrated state changes nothing.
func migrate(st *State) bool {
	changed := false
	for _, repo := range st.Repositories {
		if repo != nil && migrateRepository(repo) {
			changed = true
		}
	}
	return changed
}

func migrateRepository(repo *RepositoryState) bool {
	if len(repo.Worktrees) == 0 {
		return false
	}
	type candidate struct {
		key string
		wt  *WorktreeState
	}
	var current, legacy []candidate
	for key, wt := range repo.Worktrees {
		if wt == nil {
			continue
		}
		if wt.Branch == "" {
			legacy = append(legacy, candidate{key, wt})
		} else {
			current = append(current, candidate{key, wt})
		}
	}
	byKey := func(c []candidate) {
		sort.Slice(c, func(i, j int) bool { return c[i].key < c[j].key })
	}
	byKey(current)
	byKey(legacy)

	changed := len(legacy) > 0
	out := make(map[string]*WorktreeState, len(repo.Worktrees))
	resolved := map[string]string{} // resolved path -> key in out
	place := func(key string, wt *WorktreeState) {
		if wt.Path == "" {
			if _, taken := out[key]; taken {
				key += "#" + strconv.Itoa(len(out))
			}
			out[key] = wt
			return
		}
		r := ResolvePath(wt.Path)
		if existing, ok := resolved[r]; ok {
			out[existing] = mergeEntries(out[existing], wt)
			changed = true
			return
		}
		key = WorktreeKey(wt.Path)
		resolved[r] = key
		out[key] = wt
	}

	for _, c := range current {
		key := c.key
		if c.wt.Path != "" && WorktreeKey(c.wt.Path) != key {
			changed = true
		}
		place(key, c.wt)
	}
	for _, c := range legacy {
		c.wt.Branch = c.key
		if c.wt.HubPath == "" {
			c.wt.HubPath = hubContaining(repo, c.wt.Path)
		}
		place(legacyKeyPrefix+c.key, c.wt)
	}
	if len(out) != len(repo.Worktrees) {
		changed = true
	}
	repo.Worktrees = out
	return changed
}

// hubContaining returns the deepest recorded hub of repo whose path
// contains path, or "".
func hubContaining(repo *RepositoryState, path string) string {
	if path == "" {
		return ""
	}
	best := ""
	for _, h := range repo.Hubs {
		if h == nil || h.Path == "" || !pathWithin(path, h.Path) {
			continue
		}
		if len(h.Path) > len(best) {
			best = h.Path
		}
	}
	return best
}

// mergeEntries combines two entries for the same worktree. keep's
// non-empty fields win and its empty ones are filled from other; the
// worktree was created at the earlier time and last accessed at the later.
func mergeEntries(keep, other *WorktreeState) *WorktreeState {
	merged := *keep
	if merged.Branch == "" {
		merged.Branch = other.Branch
	}
	if merged.Type == "" {
		merged.Type = other.Type
	}
	if merged.HubPath == "" {
		merged.HubPath = other.HubPath
	}
	if merged.CreatedAt.IsZero() || (!other.CreatedAt.IsZero() && other.CreatedAt.Before(merged.CreatedAt)) {
		merged.CreatedAt = other.CreatedAt
	}
	if other.LastAccessed.After(merged.LastAccessed) {
		merged.LastAccessed = other.LastAccessed
	}
	return &merged
}
