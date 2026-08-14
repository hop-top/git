package cmd

import (
	"strings"
	"testing"

	"hop.top/git/internal/cli"
)

// TestInitChoice_UnanswerableFailsLoudly is the regression guard for the
// reported hang: `git hop init` in a convertible repo with nothing
// readable on stdin used to loop forever printing "Invalid choice.
// Please try again.", emitting tens of megabytes until the caller killed
// it. An agent or CI job running init without a terminal wedged.
//
// The prompt must instead fail loudly: non-zero exit plus a lowercase
// "fatal:" message on stderr naming the flag that makes init work
// without a prompt.
func TestInitChoice_UnanswerableFailsLoudly(t *testing.T) {
	stdout, stderr, code := runPromptScenario(t, "init-choice-unanswerable")

	if code == 0 {
		t.Fatalf("unanswerable init prompt exited 0; want non-zero.\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if code != exitPromptUnanswerable {
		t.Fatalf("unanswerable init prompt exited %d, want %d", code, exitPromptUnanswerable)
	}
	// The defining symptom of the bug: unbounded retry spew.
	if strings.Count(stdout, "Invalid choice") > 1 {
		t.Fatalf("init prompt retried without bound on unreadable stdin; stdout has %d \"Invalid choice\" lines",
			strings.Count(stdout, "Invalid choice"))
	}
	if !strings.Contains(stderr, "fatal: ") {
		t.Fatalf("stderr %q lacks the lowercase \"fatal: \" prefix", stderr)
	}
	if !strings.Contains(stderr, "--no-prompt") {
		t.Fatalf("stderr %q does not tell the user to pass --no-prompt", stderr)
	}
}

// TestInitChoice_UnanswerableIsBounded pins the size of the failure. The
// bug's signature was volume: ~100MB of retry lines in ten seconds. Even
// a fixed exit is wrong if it first floods the caller's log.
func TestInitChoice_UnanswerableIsBounded(t *testing.T) {
	stdout, _, _ := runPromptScenario(t, "init-choice-unanswerable")

	if len(stdout) > 64*1024 {
		t.Fatalf("unanswerable init prompt emitted %d bytes of output; want a bounded message", len(stdout))
	}
}

// TestInitChoice_PipedAnswerProceeds guards the scripting path. A pipe
// is not a TTY, but it HAS a readable answer, so it must keep working:
// the trigger for failing is "the prompt could not be answered", never
// "stdin is not a TTY". Report A's `printf 'y\ny\ny\n'` failed not
// because piping is ignored but because this is a 1/2/3/q choice and "y"
// is not one of them.
func TestInitChoice_PipedAnswerProceeds(t *testing.T) {
	stdout, stderr, code := runPromptScenarioStdin(t, "init-choice-piped", "1\n")

	if code != 0 {
		t.Fatalf("piped init choice exited %d, want 0.\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "chose:1") {
		t.Fatalf("piped answer not honoured; stdout %q", stdout)
	}
}

// TestInitChoice_PipedInvalidThenValid proves a piped stream may correct
// itself: invalid lines are re-prompted, and a later valid line wins.
// This is the exact shape of Report A's input followed by a real choice.
func TestInitChoice_PipedInvalidThenValid(t *testing.T) {
	stdout, stderr, code := runPromptScenarioStdin(t, "init-choice-piped", "y\ny\n2\n")

	if code != 0 {
		t.Fatalf("piped init choice exited %d, want 0.\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "chose:2") {
		t.Fatalf("piped answer after invalid lines not honoured; stdout %q", stdout)
	}
}

// TestInitChoice_PipedQuitCancels documents that an explicit quit is a
// user decision, not a failure: it stays a quiet exit 0.
func TestInitChoice_PipedQuitCancels(t *testing.T) {
	stdout, stderr, code := runPromptScenarioStdin(t, "init-choice-piped", "q\n")

	if code != 0 {
		t.Fatalf("piped quit exited %d, want 0.\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "chose:q") {
		t.Fatalf("quit not honoured; stdout %q", stdout)
	}
}

// TestInitCommand_HasNonInteractiveFlag makes sure the flag the fatal
// message tells the user to pass actually exists. A loud failure naming
// a flag the command does not accept would be worse than the hang, and
// this codebase has shipped that mistake before.
func TestInitCommand_HasNonInteractiveFlag(t *testing.T) {
	c, _, err := cli.RootCmd.Find([]string{"init"})
	if err != nil {
		t.Fatalf("init command not found: %v", err)
	}
	if c.Name() != "init" {
		t.Fatalf("resolved %q, want init", c.Name())
	}

	flag := c.Flags().Lookup("no-prompt")
	if flag == nil {
		t.Fatal("init should have a --no-prompt flag so non-interactive callers can convert")
	}
	if flag.Value.Type() != "bool" {
		t.Errorf("expected --no-prompt to be bool, got %s", flag.Value.Type())
	}
}

// TestResolveInitChoice_NoPromptPicksDefault covers the non-interactive
// path end to end at the decision level: with --no-prompt, init must
// select the recommended conversion without reading stdin at all, so it
// cannot block even when stdin is a closed pipe.
func TestResolveInitChoice_NoPromptPicksDefault(t *testing.T) {
	choice, err := resolveInitChoice(true, false)
	if err != nil {
		t.Fatalf("resolveInitChoice(--no-prompt) returned error %v, want nil", err)
	}
	if choice != choiceBareWorktree {
		t.Fatalf("resolveInitChoice(--no-prompt) = %q, want %q (the recommended conversion)", choice, choiceBareWorktree)
	}
}

// TestResolveInitChoice_NoPromptRegular pins --regular to the regular-repo
// conversion. The flag was previously declared but never read, so a
// caller passing it silently got the interactive menu anyway.
func TestResolveInitChoice_NoPromptRegular(t *testing.T) {
	choice, err := resolveInitChoice(true, true)
	if err != nil {
		t.Fatalf("resolveInitChoice(--no-prompt --regular) returned error %v, want nil", err)
	}
	if choice != choiceRegularWorktree {
		t.Fatalf("resolveInitChoice(--no-prompt --regular) = %q, want %q", choice, choiceRegularWorktree)
	}
}

// TestInitRegularFlag_IsWired is the regression guard for the dead flag:
// --regular must change the outcome. If it is ignored again, both
// branches return the same choice and this fails.
func TestInitRegularFlag_IsWired(t *testing.T) {
	bare, err := resolveInitChoice(true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	regular, err := resolveInitChoice(true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bare == regular {
		t.Fatalf("--regular is not wired: both paths chose %q", bare)
	}
}

// TestInitHints_NameRealFlags checks every `git hop init --<flag>` hint
// the command prints actually resolves to a declared flag. Two of these
// hints named flags that never existed (--current, --convert), which is
// the same class of defect as a fatal message naming a bogus flag.
func TestInitHints_NameRealFlags(t *testing.T) {
	c, _, err := cli.RootCmd.Find([]string{"init"})
	if err != nil {
		t.Fatalf("init command not found: %v", err)
	}

	for _, name := range initHintedFlags() {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("init prints a hint naming --%s, but no such flag is declared", name)
		}
	}
}
