package output

import (
	"errors"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// stdoutProfile is the colour stdout can show. Styled text is rendered
// in full colour, then brought down to it by Paint.
var stdoutProfile = colorprofile.Detect(os.Stdout, os.Environ())

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

// SetupColor decides how much colour human output on stdout carries.
// auto follows colorprofile's reading of the conventions: colour only on
// a terminal (not when piped, nor on TERM=dumb), none under NO_COLOR,
// and forced on by CLICOLOR_FORCE. always forces colour on, as git's
// --color=always does, whatever the terminal or NO_COLOR say; the depth
// still follows TERM and COLORTERM. never (--no-color) turns every
// escape code off, whatever the environment says.
func SetupColor(when ColorWhen) {
	switch when {
	case ColorNever:
		stdoutProfile = colorprofile.NoTTY
	case ColorAlways:
		stdoutProfile = colorprofile.Detect(os.Stdout, append(os.Environ(), "CLICOLOR_FORCE=1"))
	default:
		stdoutProfile = colorprofile.Detect(os.Stdout, os.Environ())
	}
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
