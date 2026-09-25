package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// ErrPromptUnanswerable reports that a confirmation prompt could not be
// put to the user at all: stdin carried no answer (the non-interactive /
// batch case) or the current output mode cannot prompt.
//
// It is deliberately distinct from an answer of "no". A declined prompt
// is a decision; an unanswerable prompt is a failed precondition, and
// callers MUST surface it as a non-zero exit rather than reporting a
// quiet cancellation that scripts read as success.
var ErrPromptUnanswerable = errors.New(
	"cannot prompt for confirmation on a non-interactive stdin; pass --no-prompt to proceed without confirmation",
)

// isPromptUnanswerable reports whether err is (or wraps)
// ErrPromptUnanswerable.
func isPromptUnanswerable(err error) bool {
	return errors.Is(err, ErrPromptUnanswerable)
}

// promptIn is the reader every prompt in this package consumes. It is a
// package var so tests can substitute a fixed script; production always
// reads real stdin.
var promptIn io.Reader = os.Stdin

// promptOut is where prompt text goes: stderr, as git's prompts do, so
// stdout carries only results. Nil means stderr; tests may substitute.
var promptOut io.Writer

func promptW() io.Writer {
	if promptOut != nil {
		return promptOut
	}
	return stderrWriter()
}

// promptReader is the buffered view of promptIn, cached so successive
// reads continue where the previous one stopped. A fresh bufio.Reader
// per call would read ahead into its buffer and then discard the
// remainder, so a second prompt would lose every line the first one
// over-read — invisible to single-prompt callers, fatal to a retry loop
// fed by a pipe. promptSrc records which reader the cache belongs to so
// a test swapping promptIn transparently invalidates it.
var (
	promptReader *bufio.Reader
	promptSrc    io.Reader
)

// bufferedPromptIn returns the buffered reader for the current promptIn,
// rebuilding it if promptIn has been swapped since the last call.
func bufferedPromptIn() *bufio.Reader {
	if promptReader == nil || promptSrc != promptIn {
		promptReader = bufio.NewReader(promptIn)
		promptSrc = promptIn
	}
	return promptReader
}

// promptInIsTerminal reports whether promptIn is a terminal. A variable
// so tests can stand in for one.
var promptInIsTerminal = func() bool {
	f, ok := promptIn.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// readPromptLine reads one answer from promptIn and ends the prompt's
// line on w. It returns ErrPromptUnanswerable when nothing at all could
// be read — EOF on an empty stdin, or a read failure. A partial final
// line without a trailing newline still counts as an answer.
//
// A terminal echoes the answer and the Enter that ends it, which closes
// the line. Nothing else does: an answer piped in is never echoed, and
// neither is an EOF, on a terminal or not. Then the line is closed here,
// so whatever is printed next (a hint:, a warning:, the next prompt)
// starts at column zero instead of after the prompt.
func readPromptLine(w io.Writer) (string, error) {
	response, err := bufferedPromptIn().ReadString('\n')
	if !strings.HasSuffix(response, "\n") || !promptInIsTerminal() {
		fmt.Fprintln(w)
	}
	if err != nil && response == "" {
		return "", ErrPromptUnanswerable
	}
	return response, nil
}

// ConfirmAnswer prompts the user for yes/no confirmation and reports
// whether the prompt could be answered at all.
//
// Returns (true, nil) on an affirmative answer, (false, nil) on any
// other answer, and (false, ErrPromptUnanswerable) when no answer could
// be read. Prefer this over Confirm in destructive code paths so a
// non-interactive run fails loudly instead of silently no-op'ing.
func ConfirmAnswer(prompt string) (bool, error) {
	if CurrentMode != ModeHuman {
		// Non-human modes have no channel to prompt on.
		return false, ErrPromptUnanswerable
	}
	w := promptW()

	fmt.Fprintf(w, "%s (y/n): ", prompt)

	response, err := readPromptLine(w)
	if err != nil {
		return false, err
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes", nil
}

// Confirm prompts the user for yes/no confirmation.
//
// It collapses an unanswerable prompt into false. Callers guarding a
// destructive action should use ConfirmAnswer instead, so they can tell
// "the user said no" apart from "nobody was there to ask".
func Confirm(prompt string) bool {
	ok, _ := ConfirmAnswer(prompt)
	return ok
}

// maxPromptRetries bounds how many invalid answers a choice prompt will
// re-ask for before giving up. A human who has mistyped this many times
// is better served by an error than another identical prompt, and it
// stops a stdin that endlessly yields unusable input from spinning.
const maxPromptRetries = 10

// ChoiceAnswer prompts the user to pick one of validChoices and reports
// whether the prompt could be answered at all. It is the multi-choice
// sibling of ConfirmAnswer and shares its contract exactly: the same
// ErrPromptUnanswerable sentinel, the same promptIn injection seam, and
// the same rule that an unanswerable prompt is a failed precondition
// rather than a decision.
//
// Returns (choice, nil) once a reply matches validChoices. Invalid
// replies are re-prompted up to maxPromptRetries times. Returns
// ("", ErrPromptUnanswerable) when stdin carries no answer at all, when
// the retry budget is exhausted, or when the output mode cannot prompt.
//
// The bound is the point. Reading to EOF must terminate: an unbounded
// retry loop turns a non-interactive run into a hang that emits retry
// lines until something kills it.
func ChoiceAnswer(prompt string, validChoices []string) (string, error) {
	if CurrentMode != ModeHuman {
		// Non-human modes have no channel to prompt on.
		return "", ErrPromptUnanswerable
	}
	w := promptW()

	for attempt := 0; attempt < maxPromptRetries; attempt++ {
		fmt.Fprint(w, prompt)

		response, err := readPromptLine(w)
		if err != nil {
			return "", err
		}

		response = strings.TrimSpace(response)
		for _, valid := range validChoices {
			if response == valid {
				return response, nil
			}
		}

		fmt.Fprintln(w, "Invalid choice. Please try again.")
	}

	// Readable but never usable: treat it as unanswerable so the caller
	// takes the same loud-failure path as an empty stdin.
	return "", ErrPromptUnanswerable
}

// ConfirmWithWarning prompts with a warning-styled message
func ConfirmWithWarning(title string, message string) bool {
	if CurrentMode != ModeHuman {
		return false
	}
	w := promptW()

	// Display warning
	warningStyle := StyleWarning.Bold(true)
	fmt.Fprintln(w)
	fmt.Fprintln(w, Paint(warningStyle, IconWarning+": "+title))
	fmt.Fprintln(w)

	if message != "" {
		fmt.Fprintln(w, Paint(StyleMuted, message))
		fmt.Fprintln(w)
	}

	return Confirm("Continue?")
}

// ConfirmDeletionAnswer prompts for confirmation of a destructive
// action and reports whether the prompt could be answered at all.
// Same contract as ConfirmAnswer.
func ConfirmDeletionAnswer(target string, details []CardField) (bool, error) {
	if CurrentMode != ModeHuman {
		return false, ErrPromptUnanswerable
	}
	w := promptW()

	// Show warning card
	card := WarningCard("Confirm Removal", append(
		[]CardField{{Key: "Target", Value: target}},
		details...,
	))

	fmt.Fprintln(w, card)
	fmt.Fprintln(w)

	warning := Paint(StyleWarning, "warning: this action cannot be undone")
	fmt.Fprintln(w, warning)
	fmt.Fprintln(w)

	return ConfirmAnswer("Continue?")
}

// ConfirmDeletion prompts for confirmation of destructive actions.
//
// Collapses an unanswerable prompt into false; prefer
// ConfirmDeletionAnswer in new code so the caller can distinguish a
// declined prompt from one that could not be asked.
func ConfirmDeletion(target string, details []CardField) bool {
	ok, _ := ConfirmDeletionAnswer(target, details)
	return ok
}

// Select prompts the user to select from a list of options
func Select(prompt string, options []string) (int, string) {
	if CurrentMode != ModeHuman {
		return -1, ""
	}
	w := promptW()

	fmt.Fprintln(w, prompt)
	fmt.Fprintln(w)

	for i, opt := range options {
		fmt.Fprintf(w, "  %d) %s\n", i+1, opt)
	}

	fmt.Fprintln(w)
	fmt.Fprint(w, "Select option: ")

	response, err := readPromptLine(w)
	if err != nil {
		return -1, ""
	}

	response = strings.TrimSpace(response)
	var selected int
	_, err = fmt.Sscanf(response, "%d", &selected)
	if err != nil || selected < 1 || selected > len(options) {
		return -1, ""
	}

	return selected - 1, options[selected-1]
}

// Input prompts for text input
func Input(prompt string) string {
	if CurrentMode != ModeHuman {
		return ""
	}
	w := promptW()

	fmt.Fprintf(w, "%s: ", prompt)

	response, err := readPromptLine(w)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(response)
}

// InputWithDefault prompts for text input with a default value
func InputWithDefault(prompt string, defaultValue string) string {
	if CurrentMode != ModeHuman {
		return defaultValue
	}
	w := promptW()

	defaultHint := Paint(StyleMuted, fmt.Sprintf(" [%s]", defaultValue))
	fmt.Fprintf(w, "%s%s: ", prompt, defaultHint)

	response, err := readPromptLine(w)
	if err != nil {
		return defaultValue
	}

	response = strings.TrimSpace(response)
	if response == "" {
		return defaultValue
	}

	return response
}

// ConfirmWithPreview shows a preview before confirming
func ConfirmWithPreview(title string, preview []string) bool {
	if CurrentMode != ModeHuman {
		return false
	}
	w := promptW()

	fmt.Fprintln(w)
	fmt.Fprintln(w, RenderHeader(title))
	fmt.Fprintln(w)

	for _, line := range preview {
		fmt.Fprintln(w, "  "+line)
	}

	fmt.Fprintln(w)
	return Confirm("Proceed?")
}
