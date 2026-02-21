package cmd_test

import (
	"strings"
	"testing"

	"hop.top/git/cmd"
	"hop.top/git/internal/cli"
)

func TestRemoveCommand_Help(t *testing.T) {
	output, err := cmd.ExecuteCommand(cli.RootCmd, "remove", "--help")

	if err != nil {
		t.Errorf("Help should not error, got: %v", err)
	}

	expectedStrings := []string{
		"Remove a hub",
		"hopspace",
		"branch",
		"Usage:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output should contain %q, got:\n%s", expected, output)
		}
	}
}

func TestRemoveCommand_Structure(t *testing.T) {
	removeCmd, _, err := cli.RootCmd.Find([]string{"remove"})
	if err != nil {
		t.Fatalf("Remove command not found: %v", err)
	}

	tests := []struct {
		name  string
		check func(*testing.T)
	}{
		{
			name: "has correct use",
			check: func(t *testing.T) {
				if removeCmd.Use != "remove [target]" {
					t.Errorf("Expected Use='remove [target]', got '%s'", removeCmd.Use)
				}
			},
		},
		{
			name: "has short description",
			check: func(t *testing.T) {
				if removeCmd.Short == "" {
					t.Error("Remove command should have a Short description")
				}
			},
		},
		{
			name: "requires exactly 1 arg",
			check: func(t *testing.T) {
				if removeCmd.Args == nil {
					t.Error("Remove should have Args validator")
					return
				}
				// Test with no args - should error
				if err := removeCmd.Args(removeCmd, []string{}); err == nil {
					t.Error("Remove should require exactly 1 arg, accepted 0")
				}
				// Test with 1 arg - should succeed
				if err := removeCmd.Args(removeCmd, []string{"test"}); err != nil {
					t.Errorf("Remove should accept 1 arg, got error: %v", err)
				}
			},
		},
		{
			name: "has --no-prompt flag",
			check: func(t *testing.T) {
				flag := removeCmd.Flags().Lookup("no-prompt")
				if flag == nil {
					t.Fatal("Remove command should have --no-prompt flag")
				}
				if flag.Value.Type() != "bool" {
					t.Errorf("Expected --no-prompt to be bool, got %s", flag.Value.Type())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}
}

func TestRemoveCommand_RequiresArgument(t *testing.T) {
	output, err := cmd.ExecuteCommand(cli.RootCmd, "remove")

	// Cobra shows help or error when required args are missing
	if err == nil && !strings.Contains(output, "Usage:") {
		t.Error("Remove without argument should error or show usage")
	}
}

func TestRemoveCommand_InheritsGlobalFlags(t *testing.T) {
	removeCmd, _, err := cli.RootCmd.Find([]string{"remove"})
	if err != nil {
		t.Fatalf("Remove command not found: %v", err)
	}

	globalFlags := []string{"verbose", "quiet", "json", "force"}

	for _, flagName := range globalFlags {
		t.Run("inherits --"+flagName, func(t *testing.T) {
			flag := removeCmd.Flags().Lookup(flagName)
			if flag == nil {
				flag = removeCmd.InheritedFlags().Lookup(flagName)
			}

			if flag == nil {
				t.Errorf("Remove should have access to --%s flag", flagName)
			}
		})
	}
}
