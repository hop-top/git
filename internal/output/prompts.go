package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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

// readPromptLine reads one answer from promptIn. It returns
// ErrPromptUnanswerable when nothing at all could be read — EOF on an
// empty stdin, or a read failure. A partial final line without a
// trailing newline still counts as an answer.
func readPromptLine() (string, error) {
	reader := bufio.NewReader(promptIn)
	response, err := reader.ReadString('\n')
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

	fmt.Printf("%s (y/n): ", prompt)

	response, err := readPromptLine()
	if err != nil {
		// Close the dangling prompt line so the following error message
		// starts at column zero.
		fmt.Println()
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

// ConfirmWithWarning prompts with a warning-styled message
func ConfirmWithWarning(title string, message string) bool {
	if CurrentMode != ModeHuman {
		return false
	}

	// Display warning
	warningStyle := StyleWarning.Bold(true)
	fmt.Println()
	fmt.Println(warningStyle.Render(IconWarning + " " + title))
	fmt.Println()

	if message != "" {
		fmt.Println(StyleMuted.Render(message))
		fmt.Println()
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

	// Show warning card
	card := WarningCard("Confirm Removal", append(
		[]CardField{{Key: "Target", Value: target}},
		details...,
	))

	fmt.Println(card)
	fmt.Println()

	warning := StyleWarning.Render("Warning: This action cannot be undone!")
	fmt.Println(warning)
	fmt.Println()

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

	fmt.Println(prompt)
	fmt.Println()

	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt)
	}

	fmt.Println()
	fmt.Print("Select option: ")

	response, err := readPromptLine()
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

	fmt.Printf("%s: ", prompt)

	response, err := readPromptLine()
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

	defaultHint := StyleMuted.Render(fmt.Sprintf(" [%s]", defaultValue))
	fmt.Printf("%s%s: ", prompt, defaultHint)

	response, err := readPromptLine()
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

	fmt.Println()
	fmt.Println(RenderHeader(title))
	fmt.Println()

	for _, line := range preview {
		fmt.Println("  " + line)
	}

	fmt.Println()
	return Confirm("Proceed?")
}
