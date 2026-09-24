package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiagnosticsFollowOutputMode drives a warning and hint that used to
// be written straight to stderr (add's automatic origin fetch failing):
// -q must drop them, and JSON mode must report them as JSON records.
func TestDiagnosticsFollowOutputMode(t *testing.T) {
	t.Parallel()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Point origin nowhere: the automatic fetch fails and add only warns.
	env.RunCommand(t, env.HubPath, "git", "config", "remote.origin.url",
		filepath.Join(env.RootDir, "gone.git"))

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "add", "feat-human")
	if code != 0 || !strings.Contains(stderr, "warning: could not fetch origin") ||
		!strings.Contains(stderr, "hint: starting from the local refs") {
		t.Fatalf("human add: exit %d, stderr %q; want 0 with the fetch warning and hint", code, stderr)
	}

	_, stderr, code = env.RunCommandWithExit(t, env.HubPath, env.BinPath, "add", "-q", "feat-quiet")
	if code != 0 || stderr != "" {
		t.Errorf("add -q: exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
	}

	_, stderr, code = env.RunCommandWithExit(t, env.HubPath, env.BinPath, "add", "--json", "feat-json")
	if code != 0 {
		t.Fatalf("add --json: exit %d, stderr %q", code, stderr)
	}
	var sawWarning bool
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("add --json: stderr line is not a JSON record: %q", line)
			continue
		}
		if rec["level"] == "warn" && strings.Contains(rec["msg"].(string), "could not fetch origin") {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Errorf("add --json: no warn record for the fetch failure in %q", stderr)
	}
}
