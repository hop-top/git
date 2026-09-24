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
func setupColorPiped(t *testing.T, env map[string]string, noColor bool) {
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
	output.SetupColor(noColor)
	os.Stdout = old
	r.Close()
	w.Close()
	t.Cleanup(func() { output.SetupColor(true) })
}

func TestColorFollowsTerminalAndEnvironment(t *testing.T) {
	output.SetupLogger(output.ModeHuman, false)
	cases := []struct {
		name    string
		env     map[string]string
		noColor bool
		want    bool
	}{
		{"piped", nil, false, false},
		{"piped with NO_COLOR", map[string]string{"NO_COLOR": "1"}, false, false},
		{"CLICOLOR_FORCE", map[string]string{"CLICOLOR_FORCE": "1"}, false, true},
		{"--no-color beats CLICOLOR_FORCE", map[string]string{"CLICOLOR_FORCE": "1"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupColorPiped(t, tc.env, tc.noColor)
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

	setupColorPiped(t, nil, false)
	plain := build()
	want := "" +
		"Branch             State    Path\n" +
		"main               active   /hub/hops/main\n" +
		"feature-long-name  missing  /hub/hops/feature-long-name\n" +
		"x                  ok       /hub/hops/x"
	if plain != want {
		t.Errorf("plain table:\n%s\nwant:\n%s", plain, want)
	}

	setupColorPiped(t, map[string]string{"CLICOLOR_FORCE": "1"}, false)
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
	setupColorPiped(t, map[string]string{"CLICOLOR_FORCE": "1"}, false)
	got := escSeq.ReplaceAllString(output.SummaryTable(map[string]string{"k": "v"}), "")
	if got != "k  v" {
		t.Errorf("summary table = %q, want %q", got, "k  v")
	}
}
