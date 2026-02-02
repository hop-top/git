package hop_test

import (
	"strings"
	"testing"

	_ "github.com/jadb/git-hop/cmd"
	"github.com/jadb/git-hop/internal/cli"
)

func TestEnvCommand_Help(t *testing.T) {
	output, err := executeCommand(cli.RootCmd, "env", "--help")

	if err != nil {
		t.Errorf("Help should not error, got: %v", err)
	}

	expectedStrings := []string{
		"Manage the environment",
		"lifecycle",
		"Usage:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output should contain %q, got:\n%s", expected, output)
		}
	}
}

func TestEnvCommand_Structure(t *testing.T) {
	envCmd, _, err := cli.RootCmd.Find([]string{"env"})
	if err != nil {
		t.Fatalf("Env command not found: %v", err)
	}

	tests := []struct {
		name  string
		check func(*testing.T)
	}{
		{
			name: "has correct use",
			check: func(t *testing.T) {
				if envCmd.Use != "env" {
					t.Errorf("Expected Use='env', got '%s'", envCmd.Use)
				}
			},
		},
		{
			name: "has short description",
			check: func(t *testing.T) {
				if envCmd.Short == "" {
					t.Error("Env command should have a Short description")
				}
			},
		},
		{
			name: "has subcommands",
			check: func(t *testing.T) {
				if !envCmd.HasSubCommands() {
					t.Error("Env should have subcommands")
				}
			},
		},
		{
			name: "has start subcommand",
			check: func(t *testing.T) {
				startCmd, _, err := envCmd.Find([]string{"start"})
				if err != nil {
					t.Error("Env should have 'start' subcommand")
					return
				}
				if startCmd.Short == "" {
					t.Error("Env start should have description")
				}
			},
		},
		{
			name: "has stop subcommand",
			check: func(t *testing.T) {
				stopCmd, _, err := envCmd.Find([]string{"stop"})
				if err != nil {
					t.Error("Env should have 'stop' subcommand")
					return
				}
				if stopCmd.Short == "" {
					t.Error("Env stop should have description")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}
}

func TestEnvCommand_Subcommands(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "env start --help",
			args:        []string{"env", "start", "--help"},
			expectError: false,
		},
		{
			name:        "env stop --help",
			args:        []string{"env", "stop", "--help"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := executeCommand(cli.RootCmd, tt.args...)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil && !strings.Contains(output, "help") {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestEnvCommand_InheritsGlobalFlags(t *testing.T) {
	envCmd, _, err := cli.RootCmd.Find([]string{"env"})
	if err != nil {
		t.Fatalf("Env command not found: %v", err)
	}

	globalFlags := []string{"verbose", "quiet", "json"}

	for _, flagName := range globalFlags {
		t.Run("inherits --"+flagName, func(t *testing.T) {
			flag := envCmd.Flags().Lookup(flagName)
			if flag == nil {
				flag = envCmd.InheritedFlags().Lookup(flagName)
			}

			if flag == nil {
				t.Errorf("Env should have access to --%s flag", flagName)
			}
		})
	}
}
