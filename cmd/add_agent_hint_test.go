package cmd

import (
	"testing"
)

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
