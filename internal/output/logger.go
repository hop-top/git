package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

type Mode int

const (
	ModeHuman Mode = iota
	ModeJSON
	ModePorcelain
	ModeQuiet
)

var (
	CurrentMode = ModeHuman
	Verbose     = false
)

// Standard Git-tone styles (minimalist)
var (
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // Red
	styleWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // Yellow
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // Green
)

// Fatal prints an error and exits, mimicking git fatal: behavior
func Fatal(msg string, args ...interface{}) {
	if CurrentMode == ModeJSON {
		jsonErr := map[string]string{"error": fmt.Sprintf(msg, args...)}
		_ = json.NewEncoder(os.Stderr).Encode(jsonErr)
	} else {
		fmt.Fprintf(os.Stderr, "fatal: %s\n", fmt.Sprintf(msg, args...))
	}
	os.Exit(1)
}

// Error prints a non-fatal error
func Error(msg string, args ...interface{}) {
	if CurrentMode == ModeQuiet {
		return
	}
	if CurrentMode == ModeJSON {
		// JSON mode usually only prints the final result, but errors go to stderr
		jsonErr := map[string]string{"error": fmt.Sprintf(msg, args...)}
		_ = json.NewEncoder(os.Stderr).Encode(jsonErr)
		return
	}
	fmt.Fprintf(os.Stderr, "error: %s\n", fmt.Sprintf(msg, args...))
}

// Info prints standard feedback (unless quiet/porcelain/json)
func Info(msg string, args ...interface{}) {
	if CurrentMode != ModeHuman {
		return
	}
	fmt.Printf(msg+"\n", args...)
}

// Debug prints verbose logs
func Debug(msg string, args ...interface{}) {
	if !Verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "debug: %s\n", fmt.Sprintf(msg, args...))
}
