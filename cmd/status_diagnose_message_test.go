package cmd

import (
	"strings"
	"testing"
)

func TestUnregisteredBareWorktreeHint(t *testing.T) {
	const repoRoot = "/Users/me/.w/foo/usp"
	got := unregisteredBareWorktreeHint(repoRoot)

	// Must mention the path so the user knows which repo is affected.
	if !strings.Contains(got, repoRoot) {
		t.Errorf("hint missing repo root %q in:\n%s", repoRoot, got)
	}
	// Must point at the missing file by name.
	if !strings.Contains(got, "hop.json") {
		t.Errorf("hint missing 'hop.json' reference in:\n%s", got)
	}
	// Must offer the actionable next step.
	if !strings.Contains(got, "git hop init") {
		t.Errorf("hint missing 'git hop init' suggestion in:\n%s", got)
	}
	// Must use the git-porcelain 'hint:' lowercase prefix on the line
	// carrying the recovery suggestion.
	if !strings.Contains(got, "hint:") {
		t.Errorf("hint missing 'hint:' prefix in:\n%s", got)
	}
}
