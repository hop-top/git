package output_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"hop.top/git/internal/output"
)

var escSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// setupColorPiped runs output.SetupColor with stdout a pipe, as when
// the output is redirected, and env set as given (unset keys cleared).
func setupColorPiped(t *testing.T, env map[string]string, when output.ColorWhen) {
	t.Helper()
	for _, k := range []string{"NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "TTY_FORCE"} {
		t.Setenv(k, env[k])
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	output.SetupColor(when)
	os.Stdout = old
	r.Close()
	w.Close()
	t.Cleanup(func() { output.SetupColor(output.ColorNever) })
}

func TestColorFollowsTerminalAndEnvironment(t *testing.T) {
	output.SetupLogger(output.ModeHuman, false)
	cases := []struct {
		name string
		env  map[string]string
		when output.ColorWhen
		want bool
	}{
		{"piped", nil, output.ColorAuto, false},
		{"piped with NO_COLOR", map[string]string{"NO_COLOR": "1"}, output.ColorAuto, false},
		{"CLICOLOR_FORCE", map[string]string{"CLICOLOR_FORCE": "1"}, output.ColorAuto, true},
		{"never beats CLICOLOR_FORCE", map[string]string{"CLICOLOR_FORCE": "1"}, output.ColorNever, false},
		{"always when piped", nil, output.ColorAlways, true},
		{"always beats NO_COLOR", map[string]string{"NO_COLOR": "1"}, output.ColorAlways, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupColorPiped(t, tc.env, tc.when)
			rendered := []string{
				output.Colorize("x", "success"),
				output.RenderHeader("x"),
				output.RenderPath("x"),
				output.RenderKeyValue("k", "v"),
				output.StatusLine("warning", "x"),
				output.SuccessCard("x", []output.CardField{{Key: "k", Value: "v"}}),
				output.Legend(map[string]string{"a": "b"}),
			}
			for i, s := range rendered {
				if got := strings.Contains(s, "\x1b"); got != tc.want {
					t.Errorf("render %d: colour = %v, want %v (%q)", i, got, tc.want, s)
				}
			}
		})
	}
}

// Columns are sized by what the terminal shows, not by the bytes of
// the colour codes around it.
func TestStatusTableAlignsByDisplayWidth(t *testing.T) {
	output.SetupLogger(output.ModeHuman, false)
	build := func() string {
		tbl := output.NewStatusTable("Branch", "State", "Path")
		tbl.AddRow("success", "main", "active", "/hub/hops/main")
		tbl.AddRow("error", "feature-long-name", "missing", "/hub/hops/feature-long-name")
		tbl.AddRow("", "x", "ok", "/hub/hops/x")
		return tbl.Render()
	}

	setupColorPiped(t, nil, output.ColorAuto)
	plain := build()
	want := "" +
		"Branch             State    Path\n" +
		"main               active   /hub/hops/main\n" +
		"feature-long-name  missing  /hub/hops/feature-long-name\n" +
		"x                  ok       /hub/hops/x"
	if plain != want {
		t.Errorf("plain table:\n%s\nwant:\n%s", plain, want)
	}

	setupColorPiped(t, map[string]string{"CLICOLOR_FORCE": "1"}, output.ColorAuto)
	colored := build()
	if !strings.Contains(colored, "\x1b") {
		t.Fatalf("forced colour table has no colour:\n%q", colored)
	}
	if got := escSeq.ReplaceAllString(colored, ""); got != want {
		t.Errorf("coloured table, codes stripped:\n%s\nwant:\n%s", got, want)
	}
}

func TestSummaryTableAlignsByDisplayWidth(t *testing.T) {
	output.SetupLogger(output.ModeHuman, false)
	setupColorPiped(t, map[string]string{"CLICOLOR_FORCE": "1"}, output.ColorAuto)
	got := escSeq.ReplaceAllString(output.SummaryTable(map[string]string{"k": "v"}), "")
	if got != "k  v" {
		t.Errorf("summary table = %q, want %q", got, "k  v")
	}
}

// ColorWhen is a flag value: git's --color=<when> takes always, auto or
// never, and refuses anything else when the flag is parsed.
func TestColorWhenFlagValue(t *testing.T) {
	for _, v := range []string{"always", "auto", "never"} {
		var w output.ColorWhen
		if err := w.Set(v); err != nil || w.String() != v {
			t.Errorf("Set(%q) = %v, value %q", v, err, w.String())
		}
	}
	var w output.ColorWhen
	if err := w.Set("bogus"); err == nil {
		t.Error("Set(bogus) accepted")
	}
}
