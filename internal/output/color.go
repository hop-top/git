package output

import (
	"errors"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// stdoutProfile is the colour stdout can show. Styled text is rendered
// in full colour, then brought down to it by Paint.
var stdoutProfile = colorprofile.Detect(os.Stdout, os.Environ())

// stderrProfile is the colour stderr can show, where prompts go.
var stderrProfile = colorprofile.Detect(os.Stderr, os.Environ())

// ColorWhen is git's --color=<when>: always, auto or never. It is a
// flag value (pflag.Value), so a bad word is refused when the flag is
// parsed, as a usage error.
type ColorWhen string

const (
	ColorAuto   ColorWhen = "auto"
	ColorAlways ColorWhen = "always"
	ColorNever  ColorWhen = "never"
)

func (w *ColorWhen) String() string { return string(*w) }
func (w *ColorWhen) Type() string   { return "when" }

func (w *ColorWhen) Set(s string) error {
	switch v := ColorWhen(s); v {
	case ColorAuto, ColorAlways, ColorNever:
		*w = v
		return nil
	}
	return errors.New("want always, auto or never")
}

// SetupColor decides how much colour human output on stdout, and prompts
// on stderr, carry, each by its own stream.
// auto follows colorprofile's reading of the conventions: colour only on
// a terminal (not when piped, nor on TERM=dumb), none under NO_COLOR,
// and forced on by CLICOLOR_FORCE. always forces colour on, as git's
// --color=always does, whatever the terminal or NO_COLOR say; the depth
// still follows TERM and COLORTERM. never (--no-color) turns every
// escape code off, whatever the environment says.
func SetupColor(when ColorWhen) {
	stdoutProfile = detectProfile(os.Stdout, when)
	stderrProfile = detectProfile(os.Stderr, when)
}

func detectProfile(f *os.File, when ColorWhen) colorprofile.Profile {
	switch when {
	case ColorNever:
		return colorprofile.NoTTY
	case ColorAlways:
		return colorprofile.Detect(f, append(os.Environ(), "CLICOLOR_FORCE=1"))
	default:
		return colorprofile.Detect(f, os.Environ())
	}
}

// stderrWriter is os.Stderr with styled text brought down to the colour
// stderr can show: text Painted for a terminal on stdout must not leave
// escape codes in a redirected stderr.
func stderrWriter() io.Writer {
	return &colorprofile.Writer{Forward: os.Stderr, Profile: stderrProfile}
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
