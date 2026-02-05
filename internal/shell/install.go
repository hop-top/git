package shell

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"
)

const (
	wrapperMarker = "# git-hop shell integration (installed by git-hop)"
)

// IsWrapperInstalled checks if the git-hop wrapper function is already installed in the RC file
func IsWrapperInstalled(fs afero.Fs, rcPath string) bool {
	content, err := afero.ReadFile(fs, rcPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), wrapperMarker)
}

// InstallWrapper appends the git-hop wrapper function to the shell RC file
// It is idempotent - won't install twice if already present
func InstallWrapper(fs afero.Fs, shellType string, rcPath string) error {
	// Check if already installed
	if IsWrapperInstalled(fs, rcPath) {
		return nil
	}

	// Generate wrapper function
	wrapperFunc := GenerateWrapperFunction(shellType)
	if wrapperFunc == "" {
		return fmt.Errorf("unsupported shell type: %s", shellType)
	}

	// Read existing content (if file exists)
	var existingContent string
	if content, err := afero.ReadFile(fs, rcPath); err == nil {
		existingContent = string(content)
	}

	// Prepare new content
	var newContent string
	if existingContent != "" {
		// Append to existing content
		newContent = existingContent
		if !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		newContent += "\n" + wrapperFunc
	} else {
		// New file
		newContent = wrapperFunc
	}

	// Write to file
	return afero.WriteFile(fs, rcPath, []byte(newContent), 0644)
}

// UninstallWrapper removes the git-hop wrapper function from the shell RC file
func UninstallWrapper(fs afero.Fs, rcPath string) error {
	content, err := afero.ReadFile(fs, rcPath)
	if err != nil {
		return fmt.Errorf("failed to read RC file: %w", err)
	}

	contentStr := string(content)

	// Find the start of the wrapper
	markerIdx := strings.Index(contentStr, wrapperMarker)
	if markerIdx == -1 {
		// Not installed, nothing to do
		return nil
	}

	// Find the end of the wrapper function
	// For bash/zsh: find the closing brace after the marker
	// For fish: find "end" after the marker
	var endIdx int
	if strings.Contains(contentStr[markerIdx:], "git-hop() {") {
		// Bash/zsh style
		endIdx = findMatchingBrace(contentStr, markerIdx)
	} else if strings.Contains(contentStr[markerIdx:], "function git-hop") {
		// Fish style
		endIdx = findFishFunctionEnd(contentStr, markerIdx)
	} else {
		return fmt.Errorf("wrapper function format not recognized")
	}

	if endIdx == -1 {
		return fmt.Errorf("could not find end of wrapper function")
	}

	// Remove the wrapper section
	newContent := contentStr[:markerIdx] + contentStr[endIdx:]

	// Clean up extra blank lines
	newContent = strings.TrimSpace(newContent) + "\n"

	return afero.WriteFile(fs, rcPath, []byte(newContent), 0644)
}

func findMatchingBrace(content string, start int) int {
	braceCount := 0
	inFunction := false

	for i := start; i < len(content); i++ {
		if content[i] == '{' {
			braceCount++
			inFunction = true
		} else if content[i] == '}' {
			braceCount--
			if inFunction && braceCount == 0 {
				// Found matching closing brace, return position after it
				// Skip to end of line
				for i < len(content) && content[i] != '\n' {
					i++
				}
				return i + 1
			}
		}
	}

	return -1
}

func findFishFunctionEnd(content string, start int) int {
	// Look for "end" keyword that closes the function
	lines := strings.Split(content[start:], "\n")
	lineOffset := start

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "end" {
			// Found end of function
			// Calculate position in original string
			for j := 0; j <= i; j++ {
				lineOffset += len(lines[j]) + 1 // +1 for newline
			}
			return lineOffset
		}
	}

	return -1
}
