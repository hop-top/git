package output

import (
	"bytes"
	"testing"

	"github.com/spf13/pflag"
)

// withStderrTerminal pins what the auto policy sees as stderr's terminal.
func withStderrTerminal(t *testing.T, tty bool) {
	t.Helper()
	prev := stderrIsTerminal
	stderrIsTerminal = func() bool { return tty }
	t.Cleanup(func() { stderrIsTerminal = prev })
}

func withQuiet(t *testing.T, q bool) {
	t.Helper()
	prev := quiet
	quiet = q
	t.Cleanup(func() { quiet = prev })
}

func withResultFormat(t *testing.T, format string) {
	t.Helper()
	prev, prevOpts := resultFormat, resultFormatOpts
	resultFormat = format
	t.Cleanup(func() { resultFormat, resultFormatOpts = prev, prevOpts })
}

// TestShowProgress pins git's progress policy: auto shows progress only
// on a terminal and never under -q; --progress forces it whatever the
// terminal or -q say; --no-progress turns it off.
func TestShowProgress(t *testing.T) {
	cases := []struct {
		name       string
		when       ProgressWhen
		tty, quiet bool
		structured bool
		want       bool
	}{
		{"auto on a terminal", ProgressAuto, true, false, false, true},
		{"auto off a terminal", ProgressAuto, false, false, false, false},
		{"auto under -q", ProgressAuto, true, true, false, false},
		{"auto under a structured format", ProgressAuto, true, false, true, false},
		{"--progress off a terminal", ProgressAlways, false, false, false, true},
		{"--progress under -q", ProgressAlways, false, true, false, true},
		{"--progress under a structured format", ProgressAlways, false, false, true, true},
		{"--no-progress on a terminal", ProgressNever, true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withStderrTerminal(t, tc.tty)
			withQuiet(t, tc.quiet)
			format := ""
			if tc.structured {
				format = "json"
			}
			withResultFormat(t, format)
			if got := ShowProgress(tc.when); got != tc.want {
				t.Errorf("ShowProgress(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

// TestBindProgressFlags pins --progress / --no-progress as one setting
// whose last occurrence wins, as git parses --[no-]progress.
func TestBindProgressFlags(t *testing.T) {
	cases := []struct {
		args []string
		want ProgressWhen
	}{
		{nil, ProgressAuto},
		{[]string{"--progress"}, ProgressAlways},
		{[]string{"--no-progress"}, ProgressNever},
		{[]string{"--progress", "--no-progress"}, ProgressNever},
		{[]string{"--no-progress", "--progress"}, ProgressAlways},
	}
	for _, tc := range cases {
		fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
		var when ProgressWhen
		BindProgressFlags(fs, &when)
		if err := fs.Parse(tc.args); err != nil {
			t.Fatalf("%v: parse: %v", tc.args, err)
		}
		if when != tc.want {
			t.Errorf("%v: got %v, want %v", tc.args, when, tc.want)
		}
	}

	// Neither flag advertises a default in --help.
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	var when ProgressWhen
	BindProgressFlags(fs, &when)
	for _, name := range []string{"progress", "no-progress"} {
		if d := fs.Lookup(name).DefValue; d != "false" {
			t.Errorf("--%s default = %q, want false", name, d)
		}
	}
}

// TestProgress_Lines pins the meter's output: git's "<title>: NN% (x/y)"
// redrawn in place, closed with ", done." when the total is reached.
func TestProgress_Lines(t *testing.T) {
	var buf bytes.Buffer
	p := newProgress(&buf, true, "Applying repairs", 2)
	p.Tick()
	p.Tick()
	p.Stop()
	want := "\rApplying repairs:  50% (1/2)\rApplying repairs: 100% (2/2), done.\n"
	if got := buf.String(); got != want {
		t.Errorf("progress = %q, want %q", got, want)
	}
}

// TestProgress_StopMidway ends the line so what follows (an error)
// starts on its own line.
func TestProgress_StopMidway(t *testing.T) {
	var buf bytes.Buffer
	p := newProgress(&buf, true, "Applying repairs", 2)
	p.Tick()
	p.Stop()
	want := "\rApplying repairs:  50% (1/2)\n"
	if got := buf.String(); got != want {
		t.Errorf("progress = %q, want %q", got, want)
	}
}

// TestProgress_Hidden writes nothing when progress is off, and a nil
// meter is safe to use.
func TestProgress_Hidden(t *testing.T) {
	var buf bytes.Buffer
	p := newProgress(&buf, false, "Applying repairs", 2)
	p.Tick()
	p.Stop()
	if buf.Len() != 0 {
		t.Errorf("hidden progress wrote %q", buf.String())
	}
	var nilP *Progress
	nilP.Tick()
	nilP.Stop()
}
