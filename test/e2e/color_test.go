package e2e

import (
	"regexp"
	"strings"
	"testing"
)

var sgrRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestListColour checks that list colours its table only when asked to
// (a terminal, or CLICOLOR_FORCE) and that its columns line up whether
// coloured or not.
func TestListColour(t *testing.T) {
	t.Parallel()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "a-much-longer-branch-name")

	run := func(extraEnv []string, args ...string) string {
		t.Helper()
		saved := env.EnvVars
		env.EnvVars = append(append([]string{}, saved...), extraEnv...)
		defer func() { env.EnvVars = saved }()
		stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, args...)
		if code != 0 {
			t.Fatalf("%v %v: exit %d, stderr %q", extraEnv, args, code, stderr)
		}
		return stdout
	}

	piped := run(nil, "list")
	if strings.Contains(piped, "\x1b") {
		t.Errorf("piped list has escape codes:\n%q", piped)
	}
	assertListAligned(t, piped)

	if out := run([]string{"NO_COLOR=1"}, "list"); strings.Contains(out, "\x1b") {
		t.Errorf("NO_COLOR list has escape codes:\n%q", out)
	}
	if out := run([]string{"CLICOLOR_FORCE=1"}, "list", "--no-color"); strings.Contains(out, "\x1b") {
		t.Errorf("--no-color list has escape codes:\n%q", out)
	}

	forced := run([]string{"CLICOLOR_FORCE=1"}, "list")
	if !strings.Contains(forced, "\x1b") {
		t.Errorf("CLICOLOR_FORCE list has no colour:\n%q", forced)
	}
	if stripped := sgrRE.ReplaceAllString(forced, ""); stripped != piped {
		t.Errorf("coloured list differs from plain once codes are stripped:\n--- coloured ---\n%s\n--- plain ---\n%s",
			stripped, piped)
	}
}

// assertListAligned checks that every table row starts each column
// where the header's column title starts, and that no column is padded
// wider than its widest value.
func assertListAligned(t *testing.T, out string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	head := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "Branch ") {
			head = i
			break
		}
	}
	if head < 0 {
		t.Fatalf("no table header in:\n%s", out)
	}
	header := lines[head]
	var starts []int
	for i := range header {
		if header[i] != ' ' && (i == 0 || header[i-1] == ' ') {
			starts = append(starts, i)
		}
	}
	var rows []string
	for _, l := range lines[head+1:] {
		if strings.TrimSpace(l) == "" {
			break
		}
		rows = append(rows, l)
	}
	if len(rows) == 0 {
		t.Fatalf("no table rows in:\n%s", out)
	}
	for c, start := range starts {
		widest := 0
		for _, r := range rows {
			if start > 0 && (start > len(r) || r[start-1] != ' ' || r[start] == ' ') {
				t.Errorf("column %d does not start at %d in row %q (header %q)", c, start, r, header)
			}
			if c+1 < len(starts) && start < len(r) {
				cell := strings.TrimRight(r[start:min(starts[c+1], len(r))], " ")
				widest = max(widest, len(cell))
			}
		}
		if c+1 < len(starts) {
			title := strings.TrimSpace(header[start:starts[c+1]])
			if pad := starts[c+1] - start - max(widest, len(title)); pad != 2 {
				t.Errorf("column %q padded by %d, want 2 (header %q)", title, pad, header)
			}
		}
	}
}
