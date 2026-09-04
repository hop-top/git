package services

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

// IgnoredCopyMarker, placed on a comment line directly above a pattern in
// any ignore file (.gitignore at any depth, .git/info/exclude, the global
// excludes file), excludes every entry that pattern ignores from the copy
// `git hop add` performs. Example:
//
//	# task-tracker state lives in the hub, never per worktree
//	#-hop-#
//	.tlc/
//
// The marker cannot share the pattern's own line: git only treats `#` as a
// comment at the start of a line, so `.tlc/ #-hop-#` would become a pattern
// matching a path literally named ".tlc/ #-hop-#" and stop ignoring .tlc/.
// The comment block above the pattern is the nearest place git leaves free.
const IgnoredCopyMarker = "#-hop-#"

// checkIgnoreBatch caps the paths passed to one `git check-ignore` call so
// a worktree with thousands of ignored entries never approaches ARG_MAX.
const checkIgnoreBatch = 500

// ignoreRule is where one ignored entry's deciding pattern lives, as
// reported by `git check-ignore -v`.
type ignoreRule struct {
	source string // ignore file path, relative to the worktree or absolute
	line   int    // 1-based line in source
}

// markedIgnoredEntries returns the subset of entries whose deciding ignore
// rule is preceded by a comment block containing IgnoredCopyMarker.
//
// It asks git which rule decided each entry rather than re-implementing
// gitignore matching: precedence between nested files, negations, and the
// global excludes file are git's business and easy to get subtly wrong.
//
// A failure to run or parse check-ignore is returned as a warning string,
// never an error: the copy proceeds without marker skips, and the caller
// tells the user why. A user relying on the marker must not be left
// guessing why a marked directory reappeared.
func markedIgnoredEntries(fs afero.Fs, g StatusRunner, worktreePath string, entries []string) (map[string]bool, []string) {
	marked := map[string]bool{}
	var warnings []string

	rules, err := resolveIgnoreRules(g, worktreePath, entries)
	if err != nil {
		return marked, []string{fmt.Sprintf("could not resolve ignore rules; %s markers not applied: %v", IgnoredCopyMarker, err)}
	}

	// Ignore files are read once each, however many entries point at them.
	lines := map[string][]string{}
	for rel, rule := range rules {
		src := rule.source
		if !filepath.IsAbs(src) {
			src = filepath.Join(worktreePath, src)
		}
		content, ok := lines[src]
		if !ok {
			data, err := afero.ReadFile(fs, src)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("could not read %s to check for %s: %v", rule.source, IgnoredCopyMarker, err))
				lines[src] = nil
				continue
			}
			content = strings.Split(string(data), "\n")
			lines[src] = content
		}
		if patternMarked(content, rule.line) {
			marked[rel] = true
		}
	}
	return marked, warnings
}

// patternMarked reports whether the comment block directly above the
// 1-based pattern line contains IgnoredCopyMarker. The block is the run of
// `#` lines immediately preceding the pattern; a blank line or another
// pattern ends it, so a marker further up never leaks onto later rules.
func patternMarked(lines []string, line int) bool {
	for i := line - 2; i >= 0 && i < len(lines); i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "#") {
			return false
		}
		if strings.Contains(l, IgnoredCopyMarker) {
			return true
		}
	}
	return false
}

// resolveIgnoreRules maps each entry to the ignore rule git says decided
// it. Entries git cannot attribute — a directory collapsed by
// --ignored=traditional whose own name matches no pattern — are absent
// from the result; there is no rule to carry a marker for them.
func resolveIgnoreRules(g StatusRunner, worktreePath string, entries []string) (map[string]ignoreRule, error) {
	rules := map[string]ignoreRule{}
	for start := 0; start < len(entries); start += checkIgnoreBatch {
		end := start + checkIgnoreBatch
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[start:end]

		// -v yields source and line; --non-matching keeps one record per
		// path so the output can be checked against the request; --no-index
		// stops an index entry from masking the rule (these paths are
		// untracked anyway). -z is unavailable: git allows it only with
		// --stdin. That is safe because ListIgnoredEntries already drops
		// the quoted paths whose line form would be ambiguous.
		args := append([]string{"check-ignore", "-v", "--non-matching", "--no-index", "--"}, batch...)
		out, err := g.RunInDir(worktreePath, "git", args...)
		if err != nil {
			return nil, err
		}
		if err := parseCheckIgnore(out, batch, rules); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

// parseCheckIgnore reads `check-ignore -v --non-matching` output, one
// record per requested path: <source>:<line>:<pattern>\t<path>, with the
// three fields before the tab empty ("::") when no rule matched.
func parseCheckIgnore(out string, batch []string, rules map[string]ignoreRule) error {
	var records []string
	if trimmed := strings.TrimRight(out, "\n"); trimmed != "" {
		records = strings.Split(trimmed, "\n")
	}
	if len(records) != len(batch) {
		return fmt.Errorf("check-ignore returned %d records for %d paths", len(records), len(batch))
	}
	for _, rec := range records {
		rule, path, ok := strings.Cut(rec, "\t")
		if !ok {
			return fmt.Errorf("check-ignore record %q: no path separator", rec)
		}
		if rule == "::" {
			continue
		}
		source, line, err := splitIgnoreRule(rule)
		if err != nil {
			return fmt.Errorf("check-ignore record %q: %w", rec, err)
		}
		rules[path] = ignoreRule{source: source, line: line}
	}
	return nil
}

// splitIgnoreRule takes <source>:<line>:<pattern> apart. Both source and
// pattern may themselves contain ':', so the line number is located as the
// first ':'-delimited run of digits that is followed by another ':' — the
// only split that leaves a valid line number in the middle.
func splitIgnoreRule(rule string) (string, int, error) {
	for i := 0; i < len(rule); i++ {
		if rule[i] != ':' {
			continue
		}
		j := strings.IndexByte(rule[i+1:], ':')
		if j < 0 {
			break
		}
		if n, err := strconv.Atoi(rule[i+1 : i+1+j]); err == nil && n > 0 {
			return rule[:i], n, nil
		}
	}
	return "", 0, fmt.Errorf("no <source>:<line>:<pattern> structure")
}
