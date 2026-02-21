package cmd_test

import (
	"strings"
	"testing"

	"hop.top/git/cmd"
	"hop.top/git/internal/cli"
)

func TestListCommand_Help(t *testing.T) {
	output, err := cmd.ExecuteCommand(cli.RootCmd, "list", "--help")

	if err != nil {
		t.Errorf("Help should not error, got: %v", err)
	}

	expectedStrings := []string{
		"List all",
		"worktrees",
		"Usage:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output should contain %q, got:\n%s", expected, output)
		}
	}
}

func TestListCommand_Structure(t *testing.T) {
	// Find list command
	listCmd, _, err := cli.RootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("List command not found: %v", err)
	}

	// Test command properties
	tests := []struct {
		name  string
		check func(*testing.T)
	}{
		{
			name: "has correct use",
			check: func(t *testing.T) {
				if listCmd.Use != "list" {
					t.Errorf("Expected Use='list', got '%s'", listCmd.Use)
				}
			},
		},
		{
			name: "has short description",
			check: func(t *testing.T) {
				if listCmd.Short == "" {
					t.Error("List command should have a Short description")
				}
			},
		},
		{
			name: "is leaf command",
			check: func(t *testing.T) {
				if listCmd.HasSubCommands() {
					t.Error("List should not have subcommands")
				}
			},
		},
		{
			name: "accepts no args",
			check: func(t *testing.T) {
				if listCmd.Args != nil {
					err := listCmd.Args(listCmd, []string{})
					if err != nil {
						t.Errorf("List should accept 0 args, got error: %v", err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}
}

func TestListCommand_FlagCombinations(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "with --verbose",
			args:        []string{"--verbose", "list"},
			expectError: false,
		},
		{
			name:        "with --quiet",
			args:        []string{"--quiet", "list"},
			expectError: false,
		},
		{
			name:        "with --json",
			args:        []string{"--json", "list"},
			expectError: false,
		},
		{
			name:        "with invalid flag",
			args:        []string{"list", "--invalid-flag"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := cmd.ExecuteCommand(cli.RootCmd, tt.args...)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if tt.expectError && !strings.Contains(output, "unknown flag") && !strings.Contains(output, "Error") {
				t.Error("Expected unknown flag error in output")
			}
		})
	}
}

func TestListCommand_InheritsGlobalFlags(t *testing.T) {
	listCmd, _, err := cli.RootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("List command not found: %v", err)
	}

	globalFlags := []string{
		"verbose",
		"quiet",
		"json",
		"config",
		"force",
		"dry-run",
		"porcelain",
	}

	for _, flagName := range globalFlags {
		t.Run("inherits --"+flagName, func(t *testing.T) {
			flag := listCmd.Flags().Lookup(flagName)
			if flag == nil {
				flag = listCmd.InheritedFlags().Lookup(flagName)
			}

			if flag == nil {
				t.Errorf("List should have access to --%s flag", flagName)
			}
		})
	}
}

func TestListCommand_ExecutionWithoutSetup(t *testing.T) {
	// This test verifies the command can at least be executed
	// even if it might error due to missing hub

	// Execute list - it will likely print a message about not being in a hub
	// but it shouldn't panic
	_, err := cmd.ExecuteCommand(cli.RootCmd, "list")

	// Error is acceptable in this test (not in a hub)
	// The important part is that the command structure is valid
	_ = err
}
