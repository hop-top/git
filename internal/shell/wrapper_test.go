package shell_test

import (
	"strings"
	"testing"

	"hop.top/git/internal/shell"
)

func TestGenerateWrapperFunction(t *testing.T) {
	tests := []struct {
		name      string
		shellType string
		wantEmpty bool
		contains  []string
	}{
		{
			name:      "bash wrapper",
			shellType: "bash",
			wantEmpty: false,
			contains: []string{
				"git-hop()",
				"HOP_WRAPPER_ACTIVE=1",
				"command git hop",
				"cd \"$current\"",
			},
		},
		{
			name:      "zsh wrapper",
			shellType: "zsh",
			wantEmpty: false,
			contains: []string{
				"git-hop()",
				"HOP_WRAPPER_ACTIVE=1",
				"command git hop",
				"cd \"$current\"",
			},
		},
		{
			name:      "fish wrapper",
			shellType: "fish",
			wantEmpty: false,
			contains: []string{
				"function git-hop",
				"env HOP_WRAPPER_ACTIVE=1",
				"command git hop",
				"cd \"$current\"",
			},
		},
		{
			name:      "unknown shell",
			shellType: "unknown",
			wantEmpty: true,
			contains:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shell.GenerateWrapperFunction(tt.shellType)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("GenerateWrapperFunction(%q) = %q, want empty string", tt.shellType, result)
				}
				return
			}

			if result == "" {
				t.Errorf("GenerateWrapperFunction(%q) returned empty string, want non-empty", tt.shellType)
			}

			for _, substr := range tt.contains {
				if !strings.Contains(result, substr) {
					t.Errorf("GenerateWrapperFunction(%q) missing substring %q", tt.shellType, substr)
				}
			}
		})
	}
}

func TestGenerateWrapperFunctionComments(t *testing.T) {
	result := shell.GenerateWrapperFunction("bash")

	// Check for installation comment
	if !strings.Contains(result, "git-hop shell integration") {
		t.Error("Wrapper function should contain installation comment")
	}
}

func TestGenerateWrapperFunctionLogic(t *testing.T) {
	result := shell.GenerateWrapperFunction("bash")

	// Check for command detection logic
	requiredLogic := []string{
		"should_cd",                    // Variable for tracking cd decision
		"add|init|clone",               // Commands that trigger cd
		"list|status|doctor",           // Commands that don't trigger cd
		"git rev-parse --show-toplevel", // Hub root detection
		"exit_code",                    // Exit code preservation
	}

	for _, logic := range requiredLogic {
		if !strings.Contains(result, logic) {
			t.Errorf("Wrapper function missing required logic: %q", logic)
		}
	}
}
