package output

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// stdoutProfile is the colour stdout can show. Styled text is rendered
// in full colour, then brought down to it by Paint.
var stdoutProfile = colorprofile.Detect(os.Stdout, os.Environ())

// SetupColor decides how much colour human output on stdout carries,
// following colorprofile's reading of the conventions: colour only on a
// terminal (not when piped, nor on TERM=dumb), none under NO_COLOR, and
// forced on by CLICOLOR_FORCE. noColor (--no-color) turns every escape
// code off, whatever the environment says.
func SetupColor(noColor bool) {
	if noColor {
		stdoutProfile = colorprofile.NoTTY
		return
	}
	stdoutProfile = colorprofile.Detect(os.Stdout, os.Environ())
}

// Paint renders s in style for stdout. Every styled string goes through
// it, never through style.Render directly, so colour obeys SetupColor.
func Paint(style lipgloss.Style, s string) string {
	rendered := style.Render(s)
	if stdoutProfile == colorprofile.TrueColor {
		return rendered
	}
	var b strings.Builder
	w := colorprofile.Writer{Forward: &b, Profile: stdoutProfile}
	_, _ = w.Write([]byte(rendered))
	return b.String()
}
