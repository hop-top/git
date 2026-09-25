package output

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// withPromptOut captures what prompts write for the rest of the test.
func withPromptOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	prev := promptOut
	var buf bytes.Buffer
	promptOut = &buf
	t.Cleanup(func() { promptOut = prev })
	return &buf
}

// withTerminalStdin makes the prompts treat stdin as a terminal, which
// echoes the answer and its Enter.
func withTerminalStdin(t *testing.T) {
	t.Helper()
	prev := isTerminal
	isTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { isTerminal = prev })
}

// promptCases runs each prompt type; tail is what its output must end
// with once the prompt line is closed, without the closing newline.
var promptCases = []struct {
	name   string
	answer string
	run    func()
	tail   string
}{
	{"ConfirmAnswer", "y\n", func() { _, _ = ConfirmAnswer("Continue?") }, "Continue? (y/n): "},
	{"Confirm", "n\n", func() { Confirm("Continue?") }, "Continue? (y/n): "},
	{"ConfirmDeletionAnswer", "y\n", func() { _, _ = ConfirmDeletionAnswer("/tmp/x", nil) }, "Continue? (y/n): "},
	{"ConfirmWithWarning", "y\n", func() { ConfirmWithWarning("Delete", "") }, "Continue? (y/n): "},
	{"ConfirmWithPreview", "y\n", func() { ConfirmWithPreview("Plan", []string{"step"}) }, "Proceed? (y/n): "},
	{"ChoiceAnswer", "2\n", func() { _, _ = ChoiceAnswer("Choose [1/2]: ", []string{"1", "2"}) }, "Choose [1/2]: "},
	{"Select", "1\n", func() { Select("Pick one", []string{"a", "b"}) }, "Select option: "},
	{"Input", "v\n", func() { Input("Name") }, "Name: "},
	{"InputWithDefault", "v\n", func() { InputWithDefault("Name", "d") }, ": "},
}

// An answer piped in is never echoed, so the prompt closes its own line:
// what is printed next starts at column zero, not after "Select option: ".
func TestPrompts_PipedAnswerEndsPromptLine(t *testing.T) {
	for _, tc := range promptCases {
		for _, answer := range []string{tc.answer, tc.answer[:len(tc.answer)-1]} {
			withMode(t, ModeHuman)
			withStdin(t, answer)
			out := withPromptOut(t)
			tc.run()
			if want := tc.tail + "\n"; !strings.HasSuffix(out.String(), want) {
				t.Errorf("%s, answer %q: output %q does not end with %q", tc.name, answer, out, want)
			}
		}
	}
}

// With no answer at all (EOF), the line is closed once.
func TestPrompts_EOFEndsPromptLine(t *testing.T) {
	for _, tc := range promptCases {
		for _, terminal := range []bool{false, true} {
			withMode(t, ModeHuman)
			withStdin(t, "")
			if terminal {
				withTerminalStdin(t)
			}
			out := withPromptOut(t)
			tc.run()
			if want := tc.tail + "\n"; !strings.HasSuffix(out.String(), want) || strings.HasSuffix(out.String(), "\n\n") {
				t.Errorf("%s, terminal=%v: output %q does not end with exactly %q", tc.name, terminal, out, want)
			}
		}
	}
}

// A terminal echoed the answer and its Enter: no extra blank line.
func TestPrompts_TerminalAnswerAddsNoNewline(t *testing.T) {
	for _, tc := range promptCases {
		withMode(t, ModeHuman)
		withStdin(t, tc.answer)
		withTerminalStdin(t)
		out := withPromptOut(t)
		tc.run()
		if !strings.HasSuffix(out.String(), tc.tail) {
			t.Errorf("%s: output %q does not end with %q", tc.name, out, tc.tail)
		}
	}
}

// A piped invalid choice is answered on its own line, and so is the
// prompt asked again.
func TestChoiceAnswer_PipedRetryOnItsOwnLine(t *testing.T) {
	withMode(t, ModeHuman)
	withStdin(t, "x\n1\n")
	out := withPromptOut(t)
	if _, err := ChoiceAnswer("Choose [1/2]: ", []string{"1", "2"}); err != nil {
		t.Fatal(err)
	}
	want := "Choose [1/2]: \nInvalid choice. Please try again.\nChoose [1/2]: \n"
	if out.String() != want {
		t.Errorf("output %q, want %q", out, want)
	}
}

// EndPromptLine applies the rule to a reader of the caller's own, as
// prompts outside this package read their answers from one.
func TestEndPromptLine(t *testing.T) {
	tests := []struct {
		answer   string
		terminal bool
		want     string
	}{
		{"y\n", false, "\n"},
		{"y", false, "\n"},
		{"", false, "\n"},
		{"y\n", true, ""},
		{"y", true, "\n"},
		{"", true, "\n"},
	}
	for _, tt := range tests {
		if tt.terminal {
			withTerminalStdin(t)
		}
		var out bytes.Buffer
		EndPromptLine(&out, strings.NewReader(tt.answer), tt.answer)
		if out.String() != tt.want {
			t.Errorf("answer %q, terminal=%v: wrote %q, want %q", tt.answer, tt.terminal, out.String(), tt.want)
		}
	}
}
