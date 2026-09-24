package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"hop.top/git/internal/output"
)

func TestSetupLogger(t *testing.T) {
	tests := []struct {
		name    string
		mode    output.Mode
		verbose bool
	}{
		{
			name:    "human mode",
			mode:    output.ModeHuman,
			verbose: false,
		},
		{
			name:    "human mode verbose",
			mode:    output.ModeHuman,
			verbose: true,
		},
		{
			name:    "json mode",
			mode:    output.ModeJSON,
			verbose: false,
		},
		{
			name:    "quiet mode",
			mode:    output.ModeQuiet,
			verbose: false,
		},
		{
			name:    "porcelain mode",
			mode:    output.ModePorcelain,
			verbose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup logger should not panic
			output.SetupLogger(tt.mode, tt.verbose)

			// Verify mode was set
			if output.CurrentMode != tt.mode {
				t.Errorf("CurrentMode = %v, want %v", output.CurrentMode, tt.mode)
			}

			// Verify verbose was set
			if output.Verbose != tt.verbose {
				t.Errorf("Verbose = %v, want %v", output.Verbose, tt.verbose)
			}

			// Verify logger was initialized
			if output.GetLogger() == nil {
				t.Error("GetLogger() returned nil")
			}
		})
	}
}

func TestLogLevelFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
	}{
		{"debug level", "debug"},
		{"info level", "info"},
		{"warn level", "warn"},
		{"warning level", "warning"},
		{"error level", "error"},
		{"fatal level", "fatal"},
		{"invalid level", "invalid"},
		{"empty level", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original
			orig := os.Getenv("GIT_HOP_LOG_LEVEL")
			defer os.Setenv("GIT_HOP_LOG_LEVEL", orig)

			// Set test value
			if tt.envValue != "" {
				os.Setenv("GIT_HOP_LOG_LEVEL", tt.envValue)
			} else {
				os.Unsetenv("GIT_HOP_LOG_LEVEL")
			}

			// Initialize should not panic
			output.SetupLogger(output.ModeHuman, false)

			// Logger should be initialized
			if output.GetLogger() == nil {
				t.Error("GetLogger() returned nil")
			}
		})
	}
}

func TestErrorOutput(t *testing.T) {
	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	output.SetupLogger(output.ModeHuman, false)
	output.Error("test error: %s", "message")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "error:") {
		t.Errorf("Expected 'error:' prefix, got: %s", output)
	}

	if !strings.Contains(output, "test error: message") {
		t.Errorf("Expected error message, got: %s", output)
	}
}

// Quiet drops feedback, never errors: git's -q is "only report errors".
func TestQuietMode(t *testing.T) {
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			output.SetupLogger(output.ModeQuiet, false)
			output.Info("should not appear")
			output.Error("still reported")
		})
	})
	if stdout != "" {
		t.Errorf("quiet stdout = %q, want empty", stdout)
	}
	if stderr != "error: still reported\n" {
		t.Errorf("quiet stderr = %q, want %q", stderr, "error: still reported\n")
	}
}

func TestInfoOutputOnlyInHumanMode(t *testing.T) {
	tests := []struct {
		name          string
		mode          output.Mode
		shouldContain bool
	}{
		{"human mode", output.ModeHuman, true},
		{"json mode", output.ModeJSON, false},
		{"quiet mode", output.ModeQuiet, false},
		{"porcelain mode", output.ModePorcelain, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			output.SetupLogger(tt.mode, false)
			output.Info("test info message")

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			buf.ReadFrom(r)
			result := buf.String()

			hasMessage := strings.Contains(result, "test info message")
			if hasMessage != tt.shouldContain {
				t.Errorf("Info in %v mode: expected contains=%v, got contains=%v",
					tt.mode, tt.shouldContain, hasMessage)
			}
		})
	}
}

func TestWithFields(t *testing.T) {
	output.SetupLogger(output.ModeJSON, false)

	fields := map[string]interface{}{
		"branch": "main",
		"repo":   "test-repo",
	}

	logger := output.WithFields(fields)
	if logger == nil {
		t.Error("WithFields() returned nil")
	}
}

func TestWithField(t *testing.T) {
	output.SetupLogger(output.ModeJSON, false)

	logger := output.WithField("key", "value")
	if logger == nil {
		t.Error("WithField() returned nil")
	}
}

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
// The logger is set up inside fn so it binds to the redirected stream.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// Warnings use git's lowercase "warning:" prefix, like Error's "error:".
func TestWarnHumanUsesGitPrefix(t *testing.T) {
	got := captureStderr(t, func() {
		output.SetupLogger(output.ModeHuman, false)
		output.Warn("disk %s", "low")
	})
	if got != "warning: disk low\n" {
		t.Errorf("Warn stderr = %q, want %q", got, "warning: disk low\n")
	}
}

// JSON mode keeps the structured logger: one JSON object per message.
func TestWarnJSONStaysStructured(t *testing.T) {
	got := captureStderr(t, func() {
		output.SetupLogger(output.ModeJSON, false)
		output.Warn("disk %s", "low")
	})
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &rec); err != nil {
		t.Fatalf("Warn stderr is not one JSON object: %q (%v)", got, err)
	}
	if rec["msg"] != "disk low" || rec["level"] != "warn" {
		t.Errorf("Warn JSON record = %v, want msg=disk low level=warn", rec)
	}
}

// Hints use git's lowercase "hint:" prefix on stderr, one per line, as
// git's advise() prints them; an empty line gets a bare "hint:".
func TestHintHumanUsesGitPrefixPerLine(t *testing.T) {
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			output.SetupLogger(output.ModeHuman, false)
			output.Hint("No remote %s.\n\n  git remote add origin <url>", "configured")
		})
	})
	want := "hint: No remote configured.\nhint:\nhint:   git remote add origin <url>\n"
	if stderr != want {
		t.Errorf("Hint stderr = %q, want %q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("Hint wrote to stdout: %q", stdout)
	}
}

func TestHintJSONStaysStructured(t *testing.T) {
	got := captureStderr(t, func() {
		output.SetupLogger(output.ModeJSON, false)
		output.Hint("run %s", "git hop init")
	})
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &rec); err != nil {
		t.Fatalf("Hint stderr is not one JSON object: %q (%v)", got, err)
	}
	if rec["msg"] != "run git hop init" || rec["kind"] != "hint" {
		t.Errorf("Hint JSON record = %v, want msg=run git hop init kind=hint", rec)
	}
}

func TestHintSilentInQuietAndPorcelain(t *testing.T) {
	for _, mode := range []output.Mode{output.ModeQuiet, output.ModePorcelain} {
		var stderr string
		stdout := captureStdout(t, func() {
			stderr = captureStderr(t, func() {
				output.SetupLogger(mode, false)
				output.Hint("should not appear")
			})
		})
		if stderr != "" || stdout != "" {
			t.Errorf("mode %v: Hint stdout=%q stderr=%q, want both empty", mode, stdout, stderr)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// Note is unprefixed feedback on stderr, for a person at a terminal only.
func TestNoteHumanOnly(t *testing.T) {
	got := captureStderr(t, func() {
		output.SetupLogger(output.ModeHuman, false)
		output.Note("hooks: installed=%d", 2)
	})
	if got != "hooks: installed=2\n" {
		t.Errorf("human Note stderr = %q", got)
	}
	for _, mode := range []output.Mode{output.ModeQuiet, output.ModePorcelain, output.ModeJSON} {
		got := captureStderr(t, func() {
			output.SetupLogger(mode, false)
			output.Note("should not appear")
		})
		if got != "" {
			t.Errorf("mode %v: Note stderr = %q, want empty", mode, got)
		}
	}
}

// ErrorCode is the last word of a failed run: printed in every mode,
// quiet included, with git's "error:" prefix, then the process exits.
func TestErrorCodeAlwaysPrints(t *testing.T) {
	if mode := os.Getenv("OUTPUT_ERRORCODE_MODE"); mode != "" {
		m := map[string]output.Mode{
			"quiet": output.ModeQuiet, "json": output.ModeJSON, "porcelain": output.ModePorcelain,
		}[mode]
		output.SetupLogger(m, false)
		output.ErrorCode(3, "dirty %s", "tree")
		return
	}
	for mode, want := range map[string]string{
		"quiet":     "error: dirty tree\n",
		"porcelain": "error: dirty tree\n",
		"json":      `{"level":"error","msg":"dirty tree"}` + "\n",
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestErrorCodeAlwaysPrints$")
		cmd.Env = append(os.Environ(), "OUTPUT_ERRORCODE_MODE="+mode)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 {
			t.Errorf("%s: err = %v, want exit status 3", mode, err)
		}
		if stderr.String() != want {
			t.Errorf("%s: stderr = %q, want %q", mode, stderr.String(), want)
		}
	}
}
