package output_test

import (
	"testing"

	"hop.top/git/internal/output"
)

// diagnostic is one output function under test, keyed by the level name
// the stream matrix uses.
var diagnostics = map[string]func(string, ...interface{}){
	"error": output.Error,
	"warn":  output.Warn,
}

// TestDiagnosticStreamMatrix pins which diagnostics reach stderr in each
// output mode, and in what form. Git keeps warning: and error: lines on
// stderr whatever format stdout is in, so porcelain and the other
// structured formats keep them too; -q drops warnings but never errors.
// Nothing here may touch stdout, which belongs to the result.
func TestDiagnosticStreamMatrix(t *testing.T) {
	const jsonErr = `{"level":"error","msg":"disk low"}` + "\n"
	const jsonWarn = `{"level":"warn","msg":"disk low"}` + "\n"
	tests := []struct {
		name  string
		mode  output.Mode
		quiet bool // -q given alongside a mode that outranks it
		level string
		want  string
	}{
		{"human/error", output.ModeHuman, false, "error", "error: disk low\n"},
		{"human/warn", output.ModeHuman, false, "warn", "warning: disk low\n"},
		{"json/error", output.ModeJSON, false, "error", jsonErr},
		{"json/warn", output.ModeJSON, false, "warn", jsonWarn},
		{"porcelain/error", output.ModePorcelain, false, "error", "error: disk low\n"},
		{"porcelain/warn", output.ModePorcelain, false, "warn", "warning: disk low\n"},
		{"quiet/error", output.ModeQuiet, false, "error", "error: disk low\n"},
		{"quiet/warn", output.ModeQuiet, false, "warn", ""},
		{"porcelain+quiet/error", output.ModePorcelain, true, "error", "error: disk low\n"},
		{"porcelain+quiet/warn", output.ModePorcelain, true, "warn", ""},
		{"json+quiet/error", output.ModeJSON, true, "error", jsonErr},
		{"json+quiet/warn", output.ModeJSON, true, "warn", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout string
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					output.SetupLogger(tt.mode, false)
					output.SetQuiet(tt.quiet)
					diagnostics[tt.level]("disk %s", "low")
				})
			})
			if stderr != tt.want {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
	output.SetupLogger(output.ModeHuman, false)
}

// Hints are advice for a person: -q drops them in every mode, JSON
// included, as it drops warnings.
func TestHintDroppedByQuietInJSON(t *testing.T) {
	got := captureStderr(t, func() {
		output.SetupLogger(output.ModeJSON, false)
		output.SetQuiet(true)
		output.Hint("should not appear")
	})
	output.SetupLogger(output.ModeHuman, false)
	if got != "" {
		t.Errorf("json -q Hint stderr = %q, want empty", got)
	}
}

// SetupLogger resets quiet, so a later run is not left silenced by an
// earlier -q.
func TestSetupLoggerResetsQuiet(t *testing.T) {
	output.SetupLogger(output.ModePorcelain, false)
	output.SetQuiet(true)
	got := captureStderr(t, func() {
		output.SetupLogger(output.ModePorcelain, false)
		output.Warn("disk %s", "low")
	})
	if got != "warning: disk low\n" {
		t.Errorf("stderr = %q, want the warning", got)
	}
}
