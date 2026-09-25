package e2e

import (
	"sort"
	"strings"
	"testing"
)

// gitflowConfig returns the gitflow.branch.* keys of the hub, one
// "<key> <value>" per line, sorted as git prints them.
func (e *gitflowEnv) gitflowConfig(t *testing.T) string {
	t.Helper()
	out, _ := e.RunCommandAllowFail(t, e.HubPath, "git", "config", "--get-regexp", `^gitflow\.branch\..*/`)
	return strings.TrimSpace(out)
}

// With git-flow enabled, move carries the branch's git-flow config (the
// base git flow start recorded, and any other gitflow.branch.<old>.* key)
// to the new name, as git branch -m does for branch.<old>.*. Branches whose
// names merely start with the old or new one (feature/a.x, feature/b.x)
// keep their own and do not count as the new name's.
func TestGitflowMove_EnabledRekeysBranchConfig(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.gitHop(t, "add", "feature/a")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/a.note", "kept")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/a.x.base", "other")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/b.x.base", "other")

	e.gitHop(t, "move", "feature/a", "feature/b")

	want := "gitflow.branch.feature/a.x.base other\n" +
		"gitflow.branch.feature/b.base main\n" +
		"gitflow.branch.feature/b.note kept\n" +
		"gitflow.branch.feature/b.x.base other"
	if got := sortedLines(e.gitflowConfig(t)); got != want {
		t.Errorf("gitflow branch config after move:\n%s\nwant:\n%s", got, want)
	}
}

// With git-flow off (the default), move leaves git-flow config alone.
func TestGitflowMove_DisabledLeavesBranchConfig(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.gitHop(t, "add", "feature/a")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/a.base", "main")

	e.gitHop(t, "move", "feature/a", "feature/b")

	if got := e.gitflowConfig(t); got != "gitflow.branch.feature/a.base main" {
		t.Errorf("gitflow branch config after move with git-flow off:\n%s", got)
	}
}

// Keys already set for the new name are not merged into: move still
// succeeds, warns, and leaves both sets as they were.
func TestGitflowMove_ExistingNewConfigLeftAlone(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.gitHop(t, "add", "feature/a")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/b.base", "stale")

	_, stderr := e.gitHop(t, "move", "feature/a", "feature/b")

	if !strings.Contains(stderr, "gitflow.branch.feature/b.* is already set") {
		t.Errorf("no warning about the existing config:\n%s", stderr)
	}
	want := "gitflow.branch.feature/a.base main\ngitflow.branch.feature/b.base stale"
	if got := sortedLines(e.gitflowConfig(t)); got != want {
		t.Errorf("gitflow branch config:\n%s\nwant:\n%s", got, want)
	}
}

// The preview names the config move only when a real run makes it, and
// changes nothing.
func TestGitflowMove_DryRun(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		name := map[bool]string{false: "default", true: "enabled"}[enabled]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := setupGitflowEnv(t)
			e.gitHop(t, "add", "feature/a")
			e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/a.base", "main")
			if enabled {
				e.enable(t)
			}

			out, _ := e.gitHop(t, "move", "feature/a", "feature/b", "-n")

			line := "[dry-run] Would move git config gitflow.branch.feature/a.* to gitflow.branch.feature/b.*"
			if got := strings.Contains(out, line); got != enabled {
				t.Errorf("preview has %q = %v, want %v\n%s", line, got, enabled, out)
			}
			if got := e.gitflowConfig(t); got != "gitflow.branch.feature/a.base main" {
				t.Errorf("dry-run changed git-flow config:\n%s", got)
			}
		})
	}
}

func sortedLines(s string) string {
	lines := strings.Split(s, "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
