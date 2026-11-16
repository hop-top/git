package services

import (
	"path"
	"strings"
)

// DefaultIgnoredCopyExcludes are tool-state directories that belong to one
// worktree and are never seeded into a new one, whatever the repo's ignore
// files say. Copying them makes a task tracker in the new worktree open the
// source's store (or a stale config shadowing the hub's), so every hub
// would otherwise have to mark them with IgnoredCopyMarker.
//
// git-hop itself writes nothing under a worktree's .hop/ (its legacy
// footprint lived in the hub root, never in a worktree), so excluding it
// drops no state git-hop relies on.
var DefaultIgnoredCopyExcludes = []string{".tlc", ".hop"}

// excludePattern is one normalised pattern and where it came from.
type excludePattern struct {
	text     string
	anchored bool // leading '/': match from the worktree root only
	builtin  bool
}

// IgnoredCopyExcluder decides which worktree-relative paths are kept out of
// the copy: DefaultIgnoredCopyExcludes plus user patterns. It composes with
// IgnoredCopyMarker; each skips independently.
type IgnoredCopyExcluder struct {
	patterns []excludePattern
}

// NewIgnoredCopyExcluder builds an excluder from the built-in defaults and
// user patterns (hop.add.copyIgnoredExclude values). Pattern syntax:
//
//   - a pattern without '/' matches any path component at any depth
//     (".tlc" matches .tlc and pkg/.tlc, and anything inside them);
//   - a pattern with '/' matches from the worktree root, as a path or a
//     path prefix ("tmp/cache"); a leading '/' anchors a single name to
//     the root too ("/dist");
//   - '*', '?' and '[...]' glob as in path.Match;
//   - a trailing '/' is ignored;
//   - a leading '!' removes an earlier pattern with the same text,
//     including a built-in default (opt back in).
//
// Blank patterns are ignored.
func NewIgnoredCopyExcluder(user []string) *IgnoredCopyExcluder {
	var pats []excludePattern
	for _, d := range DefaultIgnoredCopyExcludes {
		pats = append(pats, excludePattern{text: d, builtin: true})
	}
	for _, raw := range user {
		p := strings.TrimSpace(raw)
		neg := strings.HasPrefix(p, "!")
		p = strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(p, "!")), "/")
		anchored := strings.HasPrefix(p, "/")
		p = strings.TrimLeft(p, "/")
		if p == "" {
			continue
		}
		np := excludePattern{text: p, anchored: anchored}
		if !neg {
			pats = append(pats, np)
			continue
		}
		kept := pats[:0]
		for _, q := range pats {
			if q.text != np.text || q.anchored != np.anchored {
				kept = append(kept, q)
			}
		}
		pats = kept
	}
	return &IgnoredCopyExcluder{patterns: pats}
}

// Match reports whether rel (slash-separated, worktree-relative) or any of
// its parent directories matches an exclude pattern, and whether the
// matching pattern is a built-in default. A built-in match wins when both
// kinds match.
func (e *IgnoredCopyExcluder) Match(rel string) (matched, builtin bool) {
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	for _, pat := range e.patterns {
		if !pat.matches(segs) {
			continue
		}
		if pat.builtin {
			return true, true
		}
		matched = true
	}
	return matched, false
}

// Excluded reports whether rel is excluded by any pattern.
func (e *IgnoredCopyExcluder) Excluded(rel string) bool {
	m, _ := e.Match(rel)
	return m
}

func (p excludePattern) matches(segs []string) bool {
	if p.anchored || strings.Contains(p.text, "/") {
		for i := range segs {
			if ok, _ := path.Match(p.text, strings.Join(segs[:i+1], "/")); ok {
				return true
			}
		}
		return false
	}
	for _, s := range segs {
		if ok, _ := path.Match(p.text, s); ok {
			return true
		}
	}
	return false
}
