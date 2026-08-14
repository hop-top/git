package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"hop.top/git/internal/output"
)

// resolveConfirmation calls os.Exit, so its exit status and stderr are
// observed by re-executing this test binary as a subprocess. The child
// selects a scenario via the env var below and runs only the helper.
const promptExitScenarioEnv = "GIT_HOP_TEST_PROMPT_SCENARIO"

func TestMain(m *testing.M) {
	switch os.Getenv(promptExitScenarioEnv) {
	case "unanswerable":
		// The non-TTY batch case: nothing on stdin to answer with.
		output.CurrentMode = output.ModeHuman
		confirmed, err := output.ConfirmAnswer("Continue?")
		resolveConfirmation(confirmed, err)
		// resolveConfirmation must exit before reaching this line.
		os.Exit(0)
	case "declined":
		// A real user typing "n" is a decision, not a failure: the
		// helper reports it and hands control back to the caller.
		output.CurrentMode = output.ModeHuman
		if resolveConfirmation(false, nil) {
			os.Exit(3) // must not report "proceed" on a decline
		}
		os.Exit(0)
	case "confirmed":
		output.CurrentMode = output.ModeHuman
		if !resolveConfirmation(true, nil) {
			os.Exit(3) // must report "proceed" on a confirmation
		}
		os.Exit(0)
	case "envgc-unanswerable", "envgc-piped-yes", "envgc-piped-no":
		// env gc's own confirmation gate, driven only by stdin: empty
		// stdin is the unanswerable batch case, "n" declines, "y"
		// proceeds. The piped variants prove a non-TTY with a readable
		// answer still works.
		output.CurrentMode = output.ModeHuman
		if confirmEnvGC(false) {
			// The real command would delete here. Either way the exit
			// status stays 0; the tests tell proceed from decline by
			// the presence of "Cancelled." on stdout.
			fmt.Println("Deleting orphaned dependencies...")
		}
		os.Exit(0)
	case "init-choice-unanswerable", "init-choice-piped":
		// init's conversion menu, driven only by stdin. Empty stdin is
		// the unanswerable batch case that used to loop forever; a piped
		// choice is a real answer and must still be honoured.
		output.CurrentMode = output.ModeHuman
		choice, err := promptInitChoice()
		if err != nil {
			// Same loud-failure path the command takes.
			output.FatalCode(exitPromptUnanswerable, "%s", err.Error())
		}
		fmt.Printf("chose:%s\n", choice)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runPromptScenario re-executes the test binary in the given scenario
// with an empty stdin and returns its stderr, stdout and exit status.
func runPromptScenario(t *testing.T, scenario string) (stdout, stderr string, code int) {
	t.Helper()
	// Empty stdin reproduces a non-interactive shell with nothing to read.
	return runPromptScenarioStdin(t, scenario, "")
}

// runPromptScenarioStdin is runPromptScenario with a caller-supplied
// stdin, so scenarios can exercise the piped-answer path.
func runPromptScenarioStdin(t *testing.T, scenario, stdin string) (stdout, stderr string, code int) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestPromptScenarioSubprocessGuard")
	cmd.Env = append(os.Environ(), promptExitScenarioEnv+"="+scenario)
	cmd.Stdin = strings.NewReader(stdin)

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	code = 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("subprocess failed to run: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return outBuf.String(), errBuf.String(), code
}

// TestPromptScenarioSubprocessGuard is the no-op target the subprocess
// runs; the real work happens in TestMain before m.Run().
func TestPromptScenarioSubprocessGuard(t *testing.T) {}

// TestResolveConfirmation_UnanswerableFailsLoudly is the regression
// guard for the reported bug: on a non-interactive stdin the
// confirmation prompt must fail loudly — non-zero exit plus a stderr
// message naming --no-prompt — instead of printing "Cancelled." and
// exiting 0, which batch scripts read as a successful removal.
func TestResolveConfirmation_UnanswerableFailsLoudly(t *testing.T) {
	stdout, stderr, code := runPromptScenario(t, "unanswerable")

	if code == 0 {
		t.Fatalf("unanswerable prompt exited 0; want non-zero.\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if code != exitPromptUnanswerable {
		t.Fatalf("unanswerable prompt exited %d, want %d", code, exitPromptUnanswerable)
	}
	if strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("unanswerable prompt printed a cancellation to stdout: %q", stdout)
	}
	// git porcelain style: lowercase fatal: prefix, on stderr.
	if !strings.Contains(stderr, "fatal: ") {
		t.Fatalf("stderr %q lacks the lowercase \"fatal: \" prefix", stderr)
	}
	if !strings.Contains(stderr, "--no-prompt") {
		t.Fatalf("stderr %q does not tell the user to pass --no-prompt", stderr)
	}
}

// TestResolveConfirmation_DeclinedIsClean documents the other half of
// the contract: an actual "no" from a user is a decision, so it stays
// a quiet exit 0. Only an unaskable prompt is an error.
func TestResolveConfirmation_DeclinedIsClean(t *testing.T) {
	stdout, stderr, code := runPromptScenario(t, "declined")

	if code != 0 {
		t.Fatalf("declined prompt exited %d, want 0.\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("declined prompt should report the cancellation, got stdout %q", stdout)
	}
}

// TestResolveConfirmation_ConfirmedProceeds verifies the helper returns
// (rather than exiting) when the user confirms, so the caller runs the
// removal.
func TestResolveConfirmation_ConfirmedProceeds(t *testing.T) {
	stdout, stderr, code := runPromptScenario(t, "confirmed")

	if code != 0 {
		t.Fatalf("confirmed prompt exited %d, want 0.\nstderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("confirmed prompt reported a cancellation: %q", stdout)
	}
}
