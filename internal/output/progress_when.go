package output

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/mattn/go-isatty"
	"github.com/spf13/pflag"
)

// ProgressWhen is git's --[no-]progress setting: auto (neither flag),
// always (--progress) or never (--no-progress).
type ProgressWhen int

const (
	ProgressAuto ProgressWhen = iota
	ProgressAlways
	ProgressNever
)

func (w ProgressWhen) String() string {
	switch w {
	case ProgressAlways:
		return "always"
	case ProgressNever:
		return "never"
	}
	return "auto"
}

// progressFlag is one of the two flags writing a shared ProgressWhen.
// Both are boolean flags, so the last one on the command line wins, as
// git's --[no-]progress does.
type progressFlag struct {
	when    *ProgressWhen
	negated bool
}

// String is "true" only when this flag's own setting is in effect, so
// neither flag advertises a default in --help.
func (f progressFlag) String() string {
	if f.negated {
		return strconv.FormatBool(*f.when == ProgressNever)
	}
	return strconv.FormatBool(*f.when == ProgressAlways)
}

func (f progressFlag) Type() string     { return "bool" }
func (f progressFlag) IsBoolFlag() bool { return true }

func (f progressFlag) Set(s string) error {
	on, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	if on != f.negated {
		*f.when = ProgressAlways
	} else {
		*f.when = ProgressNever
	}
	return nil
}

// BindProgressFlags registers git's --progress and --no-progress on fs,
// both writing w.
func BindProgressFlags(fs *pflag.FlagSet, w *ProgressWhen) {
	fs.Var(progressFlag{when: w}, "progress", "force progress reporting on stderr")
	fs.Lookup("progress").NoOptDefVal = "true"
	fs.Var(progressFlag{when: w, negated: true}, "no-progress", "suppress progress reporting")
	fs.Lookup("no-progress").NoOptDefVal = "true"
}

// stderrIsTerminal reports whether stderr is a terminal. A variable so
// tests can pin it.
var stderrIsTerminal = func() bool {
	fd := os.Stderr.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// ShowProgress applies git's progress policy to w: --progress forces
// progress on, even off a terminal and under -q; --no-progress turns it
// off; auto shows it only when stderr is a terminal and neither -q nor
// a structured format is in effect.
func ShowProgress(w ProgressWhen) bool {
	switch w {
	case ProgressAlways:
		return true
	case ProgressNever:
		return false
	}
	return stderrIsTerminal() && !quiet && !IsStructured()
}

// Progress is git's progress meter for a step with a known number of
// items: "<title>: NN% (x/y)" on stderr, redrawn in place and closed with
// ", done." once the total is reached. A hidden or nil meter is a no-op,
// so callers tick it unconditionally.
type Progress struct {
	w       io.Writer
	show    bool
	title   string
	current int
	total   int
	open    bool
}

// NewProgress starts a meter on stderr; show is usually ShowProgress's
// verdict for the command's --[no-]progress.
func NewProgress(show bool, title string, total int) *Progress {
	return newProgress(os.Stderr, show, title, total)
}

func newProgress(w io.Writer, show bool, title string, total int) *Progress {
	return &Progress{w: w, show: show, title: title, total: total}
}

// Tick records one more item done and redraws the line.
func (p *Progress) Tick() {
	if p == nil || !p.show {
		return
	}
	p.current++
	writeProgressLine(p.w, p.current, p.total, p.title)
	p.open = p.current < p.total
}

// Stop ends the meter. A line left open by a step that stopped short
// (an error midway) is terminated so what follows starts on its own line.
func (p *Progress) Stop() {
	if p == nil || !p.open {
		return
	}
	fmt.Fprintln(p.w)
	p.open = false
}

// writeProgressLine draws one git-style progress update on w.
func writeProgressLine(w io.Writer, current, total int, message string) {
	percent := 100.0
	if total > 0 {
		percent = float64(current) / float64(total) * 100
	}
	fmt.Fprintf(w, "\r%s: %3.0f%% (%d/%d)", message, percent, current, total)
	if current >= total {
		fmt.Fprintln(w, ", done.")
	}
}
