package shell

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	// wrapperMarker opens the emitted block. Kept byte-identical to the
	// original marker so blocks installed before versioning are still
	// found -- they are detected as stale and rewritten rather than
	// stranded in the rc file forever.
	wrapperMarker = "# git-hop shell integration (installed by git-hop)"

	// wrapperEndMarker closes the emitted block. Its whole job is to make
	// the block's extent explicit: uninstall used to brace-match the
	// wrapper function and stop there, leaving the tab-completion block
	// that follows behind as orphaned junk.
	wrapperEndMarker = "# end git-hop shell integration"
)

// wrapperVersionRe extracts the version from a begin marker. Unversioned
// (pre-versioning) blocks do not match, and are treated as version 0.
var wrapperVersionRe = regexp.MustCompile(`# git-hop shell integration \(installed by git-hop\) v(\d+)`)

// versionedBeginMarker is the begin marker actually emitted today: the
// legacy marker with the current version appended. Legacy detection still
// works because the legacy string is a prefix of this one.
func versionedBeginMarker() string {
	return fmt.Sprintf("%s v%d", wrapperMarker, wrapperVersion)
}

// IsWrapperInstalled checks if the git-hop wrapper function is already installed in the RC file
func IsWrapperInstalled(fs afero.Fs, rcPath string) bool {
	content, err := afero.ReadFile(fs, rcPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), wrapperMarker)
}

// installedWrapperVersion reports the version of the wrapper block present
// in content, and whether a block was found at all. A block written before
// versioning existed carries no version and reports 0, which is below
// wrapperVersion and therefore stale.
func installedWrapperVersion(content string) (int, bool) {
	idx := strings.Index(content, wrapperMarker)
	if idx == -1 {
		return 0, false
	}

	if m := wrapperVersionRe.FindStringSubmatch(content[idx:]); m != nil {
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, true
		}
		return v, true
	}

	return 0, true
}

// IsWrapperCurrent reports whether the rc file carries a wrapper block at
// the version this binary emits. A block from an older generation is NOT
// current: without this check every existing installation would keep its
// original wrapper forever while reporting itself correctly configured.
func IsWrapperCurrent(fs afero.Fs, rcPath string) bool {
	content, err := afero.ReadFile(fs, rcPath)
	if err != nil {
		return false
	}

	version, found := installedWrapperVersion(string(content))
	return found && version >= wrapperVersion
}

// InstallWrapper appends the git-hop wrapper function to the shell RC file.
//
// Idempotent for a current block (no-op), and self-healing for a stale
// one: an outdated block is excised and replaced, so a user who installed
// an earlier wrapper picks up behaviour changes instead of silently
// carrying the old code.
func InstallWrapper(fs afero.Fs, shellType string, rcPath string) error {
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

	if version, found := installedWrapperVersion(existingContent); found {
		// Already current -- nothing to do.
		if version >= wrapperVersion {
			return nil
		}

		// Stale block: cut it out, then fall through and append the
		// current one. Rewriting in place keeps a single block in the
		// file rather than stacking generations.
		stripped, err := removeWrapperBlock(existingContent)
		if err != nil {
			return err
		}
		existingContent = stripped
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

	newContent, err := removeWrapperBlock(string(content))
	if err != nil {
		return err
	}

	return afero.WriteFile(fs, rcPath, []byte(newContent), 0644)
}

// removeWrapperBlock excises the git-hop block from rc file content,
// returning the content unchanged when no block is present.
//
// Blocks carrying the explicit end marker are cut by exact bounds. Older
// blocks predate that marker, so they still need the structural scan --
// and for those the completion block genuinely was emitted after the
// function, so the scan is extended past it to avoid leaving orphans.
func removeWrapperBlock(content string) (string, error) {
	markerIdx := strings.Index(content, wrapperMarker)
	if markerIdx == -1 {
		// Not installed, nothing to do
		return content, nil
	}

	endIdx := -1
	if i := strings.Index(content[markerIdx:], wrapperEndMarker); i != -1 {
		// Explicit end marker: consume it and its trailing newline.
		endIdx = markerIdx + i + len(wrapperEndMarker)
		if endIdx < len(content) && content[endIdx] == '\n' {
			endIdx++
		}
	} else if strings.Contains(content[markerIdx:], "git-hop() {") {
		// Legacy bash/zsh block
		endIdx = findLegacyBashZshEnd(content, markerIdx)
	} else if strings.Contains(content[markerIdx:], "function git-hop") {
		// Legacy fish block
		endIdx = findLegacyFishEnd(content, markerIdx)
	} else {
		return "", fmt.Errorf("wrapper function format not recognized")
	}

	if endIdx == -1 {
		return "", fmt.Errorf("could not find end of wrapper function")
	}

	newContent := content[:markerIdx] + content[endIdx:]

	// Clean up extra blank lines
	return strings.TrimSpace(newContent) + "\n", nil
}

// findLegacyBashZshEnd locates the end of a pre-end-marker bash/zsh block:
// the wrapper function's closing brace, plus the completion block that the
// old generator emitted after it.
func findLegacyBashZshEnd(content string, start int) int {
	end := findMatchingBrace(content, start)
	if end == -1 {
		return -1
	}
	return extendPastLegacyCompletion(content, end)
}

// findLegacyFishEnd locates the end of a pre-end-marker fish block: the
// function's closing `end`, plus the trailing completion line.
func findLegacyFishEnd(content string, start int) int {
	end := findFishFunctionEnd(content, start)
	if end == -1 {
		return -1
	}
	return extendPastLegacyCompletion(content, end)
}

// extendPastLegacyCompletion swallows the "# git-hop tab completion"
// section that legacy blocks emitted after the wrapper function, so
// uninstalling an old block does not leave a dangling `complete ...` line
// referring to a function that no longer exists.
func extendPastLegacyCompletion(content string, from int) int {
	rest := content[from:]
	const completionHeader = "# git-hop tab completion"

	idx := strings.Index(rest, completionHeader)
	if idx == -1 {
		return from
	}
	// Only claim it when nothing but blank lines separates it from the
	// function we just cut -- otherwise it belongs to something else.
	if strings.TrimSpace(rest[:idx]) != "" {
		return from
	}

	// Consume through the last consecutive non-blank line of the section.
	pos := from + idx
	end := pos
	for pos < len(content) {
		lineEnd := strings.IndexByte(content[pos:], '\n')
		if lineEnd == -1 {
			return len(content)
		}
		line := content[pos : pos+lineEnd]
		pos += lineEnd + 1
		if strings.TrimSpace(line) == "" {
			break
		}
		end = pos
	}
	return end
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
