package output

import (
	"strings"
	"testing"
	"time"
)

// TestChoiceAnswer covers the answerable cases: a readable reply that
// matches one of the valid choices is honoured verbatim.
func TestChoiceAnswer(t *testing.T) {
	valid := []string{"1", "2", "3", "q"}

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"first choice", "1\n", "1"},
		{"middle choice", "2\n", "2"},
		{"quit choice", "q\n", "q"},
		{"surrounding whitespace", "  3  \n", "3"},
		// No trailing newline still counts as an answer.
		{"no trailing newline", "1", "1"},
		// An invalid line is re-prompted, and the following valid line wins.
		{"invalid then valid", "y\n2\n", "2"},
		{"several invalid then valid", "y\nn\nmaybe\nq\n", "q"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withMode(t, ModeHuman)
			withStdin(t, tc.input)

			got, err := ChoiceAnswer("Choose [1/2/3/q]: ", valid)
			if err != nil {
				t.Fatalf("ChoiceAnswer returned error %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("ChoiceAnswer = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestChoiceAnswer_Unanswerable is the core regression guard for the
// reported hang: with no readable input the prompt MUST report an error
// promptly rather than looping forever printing "Invalid choice".
//
// The deadline is load-bearing. Before the fix this case spun without
// bound, so a plain assertion would wedge the suite instead of failing;
// the goroutine + timeout turns a regression into a fast failure.
func TestChoiceAnswer_Unanswerable(t *testing.T) {
	withMode(t, ModeHuman)
	withStdin(t, "")

	type result struct {
		got string
		err error
	}
	done := make(chan result, 1)
	go func() {
		got, err := ChoiceAnswer("Choose [1/2/3/q]: ", []string{"1", "2", "3", "q"})
		done <- result{got, err}
	}()

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatalf("ChoiceAnswer returned nil error on unreadable stdin; want ErrPromptUnanswerable")
		}
		if !isPromptUnanswerable(r.err) {
			t.Fatalf("error %v is not ErrPromptUnanswerable", r.err)
		}
		if r.got != "" {
			t.Fatalf("ChoiceAnswer = %q on unreadable stdin, want empty", r.got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ChoiceAnswer did not return on unreadable stdin: the prompt loops without bound")
	}
}

// TestChoiceAnswer_ExhaustedRetries guards the other unbounded case:
// stdin that keeps producing readable-but-invalid answers must also
// terminate rather than spin forever.
func TestChoiceAnswer_ExhaustedRetries(t *testing.T) {
	withMode(t, ModeHuman)
	// Far more invalid answers than any sane retry budget.
	withStdin(t, strings.Repeat("nope\n", 5000))

	done := make(chan error, 1)
	go func() {
		_, err := ChoiceAnswer("Choose [1/2/3/q]: ", []string{"1", "2", "3", "q"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ChoiceAnswer returned nil error after only invalid answers; want ErrPromptUnanswerable")
		}
		if !isPromptUnanswerable(err) {
			t.Fatalf("error %v is not ErrPromptUnanswerable", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ChoiceAnswer did not return on endlessly-invalid stdin: the prompt loops without bound")
	}
}

// TestChoiceAnswer_NonHumanMode documents that porcelain/json/quiet
// modes have no channel to prompt on, and must surface that as the same
// unanswerable error rather than silently picking a choice.
func TestChoiceAnswer_NonHumanMode(t *testing.T) {
	for _, m := range []Mode{ModeJSON, ModePorcelain, ModeQuiet} {
		withMode(t, m)
		withStdin(t, "1\n")

		got, err := ChoiceAnswer("Choose [1/2/3/q]: ", []string{"1", "2", "3", "q"})
		if err == nil {
			t.Fatalf("mode %v: ChoiceAnswer returned nil error, want ErrPromptUnanswerable", m)
		}
		if !isPromptUnanswerable(err) {
			t.Fatalf("mode %v: error %v is not ErrPromptUnanswerable", m, err)
		}
		if got != "" {
			t.Fatalf("mode %v: ChoiceAnswer = %q, want empty", m, got)
		}
	}
}

// TestChoiceAnswer_SharesUnanswerableError pins the mechanism: the
// multi-choice prompt reports the SAME sentinel as the yes/no prompt, so
// callers have one error to branch on rather than two parallel systems.
func TestChoiceAnswer_SharesUnanswerableError(t *testing.T) {
	withMode(t, ModeHuman)
	withStdin(t, "")
	_, choiceErr := ChoiceAnswer("Choose: ", []string{"1"})

	withStdin(t, "")
	_, confirmErr := ConfirmAnswer("Continue?")

	if !isPromptUnanswerable(choiceErr) || !isPromptUnanswerable(confirmErr) {
		t.Fatalf("both prompts must report ErrPromptUnanswerable; got choice=%v confirm=%v", choiceErr, confirmErr)
	}
}
