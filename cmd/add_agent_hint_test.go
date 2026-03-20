package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestOpencodeAgentHint_LocalConfig(t *testing.T) {
	r, w, _ := os.Pipe()
	w.WriteString("1\n")
	w.Close()

	// Capture stderr
	origStderr := os.Stderr
	sr, sw, _ := os.Pipe()
	os.Stderr = sw

	opencodeAgentHint("/some/worktree", r)

	sw.Close()
	os.Stderr = origStderr

	var buf strings.Builder
	io.Copy(&buf, sr)
	out := buf.String()

	if !strings.Contains(out, ".opencode/opencode.jsonc") {
		t.Errorf("expected local config path, got: %s", out)
	}
	if !strings.Contains(out, "/some/worktree") {
		t.Errorf("expected worktree path in output, got: %s", out)
	}
}

func TestOpencodeAgentHint_GlobalConfig(t *testing.T) {
	r, w, _ := os.Pipe()
	w.WriteString("2\n")
	w.Close()

	origStderr := os.Stderr
	sr, sw, _ := os.Pipe()
	os.Stderr = sw

	opencodeAgentHint("/some/worktree", r)

	sw.Close()
	os.Stderr = origStderr

	var buf strings.Builder
	io.Copy(&buf, sr)
	out := buf.String()

	if !strings.Contains(out, "opencode/opencode.jsonc") {
		t.Errorf("expected global config path, got: %s", out)
	}
	if strings.Contains(out, ".opencode/opencode.jsonc") && !strings.Contains(out, "config") {
		t.Errorf("expected global (not local) config path, got: %s", out)
	}
	if !strings.Contains(out, "/some/worktree") {
		t.Errorf("expected worktree path in output, got: %s", out)
	}
}

func TestAgentDirHint(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string
	}{
		{
			name: "claude code",
			env:  map[string]string{"CLAUDE_CODE": "1"},
			want: "/add-dir /some/path",
		},
		{
			name: "gemini cli",
			env:  map[string]string{"GEMINI_CLI": "1"},
			want: "/directory add /some/path",
		},
		{
			name: "github copilot cli",
			env:  map[string]string{"COPILOT_GH": "true"},
			want: "/add-dir /some/path",
		},
		{
			name: "no agent",
			env:  map[string]string{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars then set test ones
			for _, k := range []string{"CLAUDE_CODE", "GEMINI_CLI", "COPILOT_GH"} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got := agentDirHint("/some/path")
			if got != tt.want {
				t.Errorf("agentDirHint() = %q, want %q", got, tt.want)
			}
		})
	}
}
