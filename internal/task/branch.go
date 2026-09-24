// Package task turns a task id into what `git hop add --task` needs: the
// task's metadata, looked up with the tlc CLI, and a branch name derived
// from it.
package task

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Task is the subset of a task's metadata that names a branch.
type Task struct {
	ID    string
	Title string
	Tags  []string
}

// MaxSlugLen bounds the slug part of a derived branch name.
const MaxSlugLen = 48

// defaultType is the branch type used when no tag names a known one.
const defaultType = "feat"

// commitTypes maps type:<x> tag values to Conventional Commits types.
// Identity entries are the spec's own types; the rest are common aliases.
var commitTypes = map[string]string{
	"feat": "feat", "fix": "fix", "docs": "docs", "style": "style",
	"refactor": "refactor", "perf": "perf", "test": "test", "build": "build",
	"ci": "ci", "chore": "chore", "revert": "revert",

	"feature": "feat", "bug": "fix", "bugfix": "fix", "hotfix": "fix",
	"doc": "docs", "tests": "test", "performance": "perf",
}

// BranchName derives "<type>/<slug>" from t's type tag and title. The
// task id is deliberately not part of it.
func BranchName(t Task) (string, error) {
	slug := Slug(t.Title)
	if slug == "" {
		return "", fmt.Errorf("task %s has no title to name a branch after", t.ID)
	}
	return CommitType(t.Tags) + "/" + slug, nil
}

// CommitType returns the Conventional Commits type named by the first
// type:<x> tag it recognizes, or "feat".
func CommitType(tags []string) string {
	for _, tag := range tags {
		key, val, ok := strings.Cut(tag, ":")
		if !ok || !strings.EqualFold(key, "type") {
			continue
		}
		if t, ok := commitTypes[strings.ToLower(strings.TrimSpace(val))]; ok {
			return t
		}
	}
	return defaultType
}

// Slug renders title as lowercase ASCII words joined by single hyphens,
// at most MaxSlugLen long. Accents are folded to their base letter;
// anything else outside [a-z0-9] separates words. A slug that would
// exceed the limit is cut at the last word boundary that fits.
func Slug(title string) string {
	var b strings.Builder
	pendingSep := false
	for _, r := range norm.NFD.String(title) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// Combining mark left by NFD: drop it, keep the base letter.
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if pendingSep && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingSep = false
			b.WriteRune(unicode.ToLower(r))
		default:
			pendingSep = true
		}
	}
	return truncateSlug(b.String())
}

func truncateSlug(s string) string {
	if len(s) <= MaxSlugLen {
		return s
	}
	cut := s[:MaxSlugLen]
	if s[MaxSlugLen] == '-' {
		return cut
	}
	if i := strings.LastIndexByte(cut, '-'); i > 0 {
		return cut[:i]
	}
	return cut
}

// ValidateID rejects ids that could not be a task id and would be unsafe
// to hand to another command: empty, flag-like, or containing whitespace
// or control characters.
func ValidateID(id string) error {
	switch {
	case id == "":
		return errors.New("task id is empty")
	case strings.HasPrefix(id, "-"):
		return fmt.Errorf("invalid task id %q: must not start with '-'", id)
	case strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return fmt.Errorf("invalid task id %q: must not contain whitespace or control characters", id)
	}
	return nil
}
