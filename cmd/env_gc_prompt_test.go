package cmd

import (
	"strings"
	"testing"

	"hop.top/git/internal/cli"
)

// TestEnvGCConfirm_UnanswerableFailsLoudly is the regression guard for
// the reported bug: `git hop env gc` on a non-interactive stdin used to
// print "Cancelled." to stdout and exit 0, which a batch script reads as
// a successful garbage collection that silently did nothing.
//
// The prompt must instead route through the shared confirmation helper
// and fail loudly: non-zero exit plus a lowercase "fatal:" message on
// stderr naming the non-interactive flag.
func TestEnvGCConfirm_UnanswerableFailsLoudly(t *testing.T) {
	stdout, stderr, code := runPromptScenario(t, "envgc-unanswerable")

	if code == 0 {
		t.Fatalf("unanswerable env gc prompt exited 0; want non-zero.\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if code != exitPromptUnanswerable {
		t.Fatalf("unanswerable env gc prompt exited %d, want %d", code, exitPromptUnanswerable)
	}
	if strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("unanswerable env gc prompt printed a cancellation to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "fatal: ") {
		t.Fatalf("stderr %q lacks the lowercase \"fatal: \" prefix", stderr)
	}
	if !strings.Contains(stderr, "--no-prompt") {
		t.Fatalf("stderr %q does not tell the user to pass --no-prompt", stderr)
	}
}

// TestEnvGCConfirm_PipedYesProceeds guards the scripting path: `echo y |
// git hop env gc` is also a non-TTY, but it HAS a readable answer and
// must keep working. The trigger is "the prompt could not be answered",
// never "stdin is not a TTY".
func TestEnvGCConfirm_PipedYesProceeds(t *testing.T) {
	stdout, stderr, code := runPromptScenarioStdin(t, "envgc-piped-yes", "y\n")

	if code != 0 {
		t.Fatalf("piped-yes env gc prompt exited %d, want 0.\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("piped-yes env gc prompt reported a cancellation: %q", stdout)
	}
}

// TestEnvGCConfirm_PipedNoDeclines documents the other half of the
// contract: an explicit "n" is a user decision, not a failure, so it
// stays a quiet exit 0 with the cancellation reported on stdout. Only
// an unanswerable prompt is an error.
func TestEnvGCConfirm_PipedNoDeclines(t *testing.T) {
	stdout, stderr, code := runPromptScenarioStdin(t, "envgc-piped-no", "n\n")

	if code != 0 {
		t.Fatalf("piped-no env gc prompt exited %d, want 0.\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("piped-no env gc prompt should report the cancellation, got stdout %q", stdout)
	}
}

// TestEnvGCCommand_HasNonInteractiveFlag makes sure the flag the fatal
// message tells the user to pass actually exists. A loud failure naming
// a flag the command does not accept would be worse than the bug.
func TestEnvGCCommand_HasNonInteractiveFlag(t *testing.T) {
	gcCmd, _, err := cli.RootCmd.Find([]string{"env", "gc"})
	if err != nil {
		t.Fatalf("env gc command not found: %v", err)
	}

	for _, name := range []string{"no-prompt", "force"} {
		flag := gcCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("env gc should have a --%s flag", name)
		}
		if flag.Value.Type() != "bool" {
			t.Errorf("expected --%s to be bool, got %s", name, flag.Value.Type())
		}
	}
}
