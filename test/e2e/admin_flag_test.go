package e2e

import (
	"strings"
	"testing"
)

// TestHelp_HidesAdminCommands verifies that --help does not list
// completion, upgrade, or help in the available commands section.
func TestHelp_HidesAdminCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	out := env.RunGitHopCombined(t, env.RootDir, "--help")

	for _, hidden := range []string{"completion", "upgrade"} {
		// Check in the "Available Commands" section — look for it as a command entry
		// (indented with spaces, followed by spaces and description).
		// A simple contains check is sufficient: the command name should not appear
		// as a listed subcommand.
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, hidden+" ") || trimmed == hidden {
				t.Errorf("--help should not list %q, but found line: %q", hidden, line)
			}
		}
	}
}

// TestAdminFlag_ShowsHiddenCommands verifies that --admin (no subcommand)
// lists completion and upgrade.
func TestAdminFlag_ShowsHiddenCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	out := env.RunGitHopCombined(t, env.RootDir, "--admin")

	for _, cmd := range []string{"completion", "upgrade"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("--admin output should list %q, got:\n%s", cmd, out)
		}
	}
}

// TestAdminFlag_NotAvailableOnSubcommand verifies that passing --admin to a
// subcommand is rejected as an unknown flag.
func TestAdminFlag_NotAvailableOnSubcommand(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	out := env.RunGitHopCombined(t, env.RootDir, "list", "--admin")

	if !strings.Contains(out, "unknown flag") && !strings.Contains(out, "unknown shorthand") {
		t.Errorf("expected unknown flag error for 'list --admin', got:\n%s", out)
	}
}

// TestCompletion_StillWorksDirectly verifies that the completion command
// still functions even though it is hidden from help.
func TestCompletion_StillWorksDirectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	out := env.RunGitHopCombined(t, env.RootDir, "completion", "bash")

	if !strings.Contains(out, "bash") && !strings.Contains(out, "complete") {
		t.Errorf("completion bash should produce bash completion script, got:\n%s", out)
	}
}
