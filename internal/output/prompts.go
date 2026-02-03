package output

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Confirm prompts the user for yes/no confirmation
func Confirm(prompt string) bool {
	if CurrentMode != ModeHuman {
		// Non-interactive modes always return false
		return false
	}

	fmt.Printf("%s (y/n): ", prompt)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
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

// ConfirmDeletion prompts for confirmation of destructive actions
func ConfirmDeletion(target string, details []CardField) bool {
	if CurrentMode != ModeHuman {
		return false
	}

	// Show warning card
	card := WarningCard("Confirm Removal", append(
		[]CardField{{Key: "Target", Value: target}},
		details...,
	))

	fmt.Println(card)
	fmt.Println()

	warning := StyleWarning.Render("⚠ Warning: This action cannot be undone!")
	fmt.Println(warning)
	fmt.Println()

	return Confirm("Continue?")
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

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
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

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
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

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
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
