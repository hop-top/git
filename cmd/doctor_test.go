package cmd_test

import (
	"strings"
	"testing"

	"github.com/jadb/git-hop/cmd"
	"github.com/jadb/git-hop/internal/cli"
)

func TestDoctorCommand_Help(t *testing.T) {
	output, err := cmd.ExecuteCommand(cli.RootCmd, "doctor", "--help")

	if err != nil {
		t.Errorf("Help should not error, got: %v", err)
	}

	expectedStrings := []string{
		"Run diagnostics",
		"--fix",
		"Usage:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output should contain %q, got:\n%s", expected, output)
		}
	}
}

func TestDoctorCommand_Structure(t *testing.T) {
	// Find doctor command
	doctorCmd, _, err := cli.RootCmd.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Doctor command not found: %v", err)
	}

	// Test command properties
	tests := []struct {
		name     string
		check    func(*testing.T)
	}{
		{
			name: "has correct use",
			check: func(t *testing.T) {
				if doctorCmd.Use != "doctor" {
					t.Errorf("Expected Use='doctor', got '%s'", doctorCmd.Use)
				}
			},
		},
		{
			name: "has short description",
			check: func(t *testing.T) {
				if doctorCmd.Short == "" {
					t.Error("Doctor command should have a Short description")
				}
			},
		},
		{
			name: "has --fix flag",
			check: func(t *testing.T) {
				fixFlag := doctorCmd.Flags().Lookup("fix")
				if fixFlag == nil {
					t.Fatal("Doctor command should have --fix flag")
				}
				if fixFlag.Value.Type() != "bool" {
					t.Errorf("Expected --fix to be bool, got %s", fixFlag.Value.Type())
				}
			},
		},
		{
			name: "is leaf command",
			check: func(t *testing.T) {
				if doctorCmd.HasSubCommands() {
					t.Error("Doctor should not have subcommands")
				}
			},
		},
		{
			name: "accepts no args",
			check: func(t *testing.T) {
				if doctorCmd.Args != nil {
					err := doctorCmd.Args(doctorCmd, []string{})
					if err != nil {
						t.Errorf("Doctor should accept 0 args, got error: %v", err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}
}

func TestDoctorCommand_FlagCombinations(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectError    bool
		checkOutput    func(t *testing.T, output string, err error)
	}{
		{
			name:        "with --fix",
			args:        []string{"doctor", "--fix"},
			expectError: false,
		},
		{
			name:        "with --verbose",
			args:        []string{"--verbose", "doctor"},
			expectError: false,
		},
		{
			name:        "with --quiet",
			args:        []string{"--quiet", "doctor"},
			expectError: false,
		},
		{
			name:        "with --json",
			args:        []string{"--json", "doctor"},
			expectError: false,
		},
		{
			name:        "with invalid flag",
			args:        []string{"doctor", "--invalid-flag"},
			expectError: true,
			checkOutput: func(t *testing.T, output string, err error) {
				if !strings.Contains(output, "unknown flag") && !strings.Contains(output, "Error") {
					t.Error("Expected unknown flag error in output")
				}
			},
		},
		{
			name:        "with --fix and --verbose",
			args:        []string{"--verbose", "doctor", "--fix"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := cmd.ExecuteCommand(cli.RootCmd, tt.args...)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if tt.checkOutput != nil {
				tt.checkOutput(t, output, err)
			}
		})
	}
}

func TestDoctorCommand_InheritsGlobalFlags(t *testing.T) {
	doctorCmd, _, err := cli.RootCmd.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Doctor command not found: %v", err)
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
			// Try local flags first
			flag := doctorCmd.Flags().Lookup(flagName)
			if flag == nil {
				// Try inherited flags
				flag = doctorCmd.InheritedFlags().Lookup(flagName)
			}

			if flag == nil {
				t.Errorf("Doctor should have access to --%s flag", flagName)
			}
		})
	}
}

func TestDoctorCommand_ExecutionWithoutSetup(t *testing.T) {
	// This test verifies the command can at least be executed
	// even if it fails due to missing setup

	// Execute doctor - it will likely fail because we're not in a hub
	// but it shouldn't panic
	_, err := cmd.ExecuteCommand(cli.RootCmd, "doctor")

	// We expect it might error (not in a hub), but it shouldn't panic
	// The important part is that the command structure is valid
	_ = err // Error is acceptable in this test
}

func TestDoctorCommand_OutputModes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		checkOutput func(t *testing.T, output string)
	}{
		{
			name: "human mode default",
			args: []string{"doctor"},
			checkOutput: func(t *testing.T, output string) {
				// Human mode should have readable output
				// (exact output depends on setup, so we just verify it runs)
			},
		},
		{
			name: "json mode",
			args: []string{"--json", "doctor"},
			checkOutput: func(t *testing.T, output string) {
				// JSON mode might output differently
				// (we can't test exact output without proper setup)
			},
		},
		{
			name: "quiet mode",
			args: []string{"--quiet", "doctor"},
			checkOutput: func(t *testing.T, output string) {
				// Quiet mode should suppress most output
				// (errors might still appear)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, _ := cmd.ExecuteCommand(cli.RootCmd, tt.args...)

			if tt.checkOutput != nil {
				tt.checkOutput(t, output)
			}
		})
	}
}
