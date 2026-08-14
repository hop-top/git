package output

import (
	"strings"
	"testing"
)

// withStdin swaps the package prompt reader for the duration of a test.
func withStdin(t *testing.T, in string) {
	t.Helper()
	prev := promptIn
	promptIn = strings.NewReader(in)
	t.Cleanup(func() { promptIn = prev })
}

func withMode(t *testing.T, m Mode) {
	t.Helper()
	prev := CurrentMode
	CurrentMode = m
	t.Cleanup(func() { CurrentMode = prev })
}

// TestConfirmAnswer covers the answerable cases: a readable reply is
// honoured verbatim, whether it says yes or no.
func TestConfirmAnswer(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"y", "y\n", true},
		{"yes", "yes\n", true},
		{"uppercase Y", "Y\n", true},
		{"n", "n\n", false},
		{"no", "no\n", false},
		{"empty line", "\n", false},
		{"garbage", "maybe\n", false},
		// No trailing newline still counts as an answer: the reader hits
		// EOF but returns the bytes it read.
		{"no trailing newline", "y", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withMode(t, ModeHuman)
			withStdin(t, tc.input)

			got, err := ConfirmAnswer("Continue?")
			if err != nil {
				t.Fatalf("ConfirmAnswer returned error %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("ConfirmAnswer = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestConfirmAnswer_Unanswerable is the core regression guard for the
// silent-cancel bug: with no readable input (the non-TTY batch case) the
// prompt MUST report an error rather than quietly returning false, which
// callers previously rendered as "Cancelled." on a zero exit.
func TestConfirmAnswer_Unanswerable(t *testing.T) {
	withMode(t, ModeHuman)
	withStdin(t, "")

	got, err := ConfirmAnswer("Continue?")
	if err == nil {
		t.Fatalf("ConfirmAnswer returned nil error on unreadable stdin; want ErrPromptUnanswerable")
	}
	if got {
		t.Fatalf("ConfirmAnswer = true on unreadable stdin, want false")
	}
	if !isPromptUnanswerable(err) {
		t.Fatalf("error %v is not ErrPromptUnanswerable", err)
	}
	// The message must tell the user how to proceed non-interactively.
	if !strings.Contains(err.Error(), "--no-prompt") {
		t.Fatalf("error %q does not mention --no-prompt", err.Error())
	}
}

// TestConfirmAnswer_NonHumanMode documents that porcelain/json/quiet
// modes cannot prompt at all, and must surface that as the same
// unanswerable error instead of a silent false.
func TestConfirmAnswer_NonHumanMode(t *testing.T) {
	for _, m := range []Mode{ModeJSON, ModePorcelain, ModeQuiet} {
		withMode(t, m)
		withStdin(t, "y\n")

		got, err := ConfirmAnswer("Continue?")
		if err == nil {
			t.Fatalf("mode %v: ConfirmAnswer returned nil error, want ErrPromptUnanswerable", m)
		}
		if got {
			t.Fatalf("mode %v: ConfirmAnswer = true, want false", m)
		}
		if !isPromptUnanswerable(err) {
			t.Fatalf("mode %v: error %v is not ErrPromptUnanswerable", m, err)
		}
	}
}

// TestConfirmDeletionAnswer wires the destructive-action card prompt to
// the same contract: answerable input is honoured, unanswerable input
// errors instead of silently declining.
func TestConfirmDeletionAnswer(t *testing.T) {
	t.Run("answered yes", func(t *testing.T) {
		withMode(t, ModeHuman)
		withStdin(t, "y\n")

		got, err := ConfirmDeletionAnswer("feature", nil)
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if !got {
			t.Fatalf("ConfirmDeletionAnswer = false, want true")
		}
	})

	t.Run("answered no", func(t *testing.T) {
		withMode(t, ModeHuman)
		withStdin(t, "n\n")

		got, err := ConfirmDeletionAnswer("feature", nil)
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
		if got {
			t.Fatalf("ConfirmDeletionAnswer = true, want false")
		}
	})

	t.Run("unanswerable", func(t *testing.T) {
		withMode(t, ModeHuman)
		withStdin(t, "")

		got, err := ConfirmDeletionAnswer("feature", nil)
		if err == nil {
			t.Fatalf("ConfirmDeletionAnswer returned nil error on unreadable stdin")
		}
		if got {
			t.Fatalf("ConfirmDeletionAnswer = true on unreadable stdin")
		}
		if !isPromptUnanswerable(err) {
			t.Fatalf("error %v is not ErrPromptUnanswerable", err)
		}
	})
}
