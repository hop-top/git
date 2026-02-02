package hop_test

import (
	"strings"
	"testing"

	_ "github.com/jadb/git-hop/cmd"
	"github.com/jadb/git-hop/internal/cli"
)

func TestPruneCommand_Help(t *testing.T) {
	output, err := executeCommand(cli.RootCmd, "prune", "--help")

	if err != nil {
		t.Errorf("Help should not error, got: %v", err)
	}

	expectedStrings := []string{
		"Remove orphaned",
		"data",
		"Usage:",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output should contain %q, got:\n%s", expected, output)
		}
	}
}

func TestPruneCommand_Structure(t *testing.T) {
	pruneCmd, _, err := cli.RootCmd.Find([]string{"prune"})
	if err != nil {
		t.Fatalf("Prune command not found: %v", err)
	}

	tests := []struct {
		name  string
		check func(*testing.T)
	}{
		{
			name: "has correct use",
			check: func(t *testing.T) {
				if pruneCmd.Use != "prune" {
					t.Errorf("Expected Use='prune', got '%s'", pruneCmd.Use)
				}
			},
		},
		{
			name: "has short description",
			check: func(t *testing.T) {
				if pruneCmd.Short == "" {
					t.Error("Prune command should have a Short description")
				}
			},
		},
		{
			name: "is leaf command",
			check: func(t *testing.T) {
				if pruneCmd.HasSubCommands() {
					t.Error("Prune should not have subcommands")
				}
			},
		},
		{
			name: "accepts no args",
			check: func(t *testing.T) {
				if pruneCmd.Args != nil {
					err := pruneCmd.Args(pruneCmd, []string{})
					if err != nil {
						t.Errorf("Prune should accept 0 args, got error: %v", err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}
}

func TestPruneCommand_InheritsGlobalFlags(t *testing.T) {
	pruneCmd, _, err := cli.RootCmd.Find([]string{"prune"})
	if err != nil {
		t.Fatalf("Prune command not found: %v", err)
	}

	globalFlags := []string{"verbose", "quiet", "json", "dry-run"}

	for _, flagName := range globalFlags {
		t.Run("inherits --"+flagName, func(t *testing.T) {
			flag := pruneCmd.Flags().Lookup(flagName)
			if flag == nil {
				flag = pruneCmd.InheritedFlags().Lookup(flagName)
			}

			if flag == nil {
				t.Errorf("Prune should have access to --%s flag", flagName)
			}
		})
	}
}

func TestPruneCommand_ExecutionWithoutSetup(t *testing.T) {
	// Execute prune - will likely print message about not being in a hub
	// but shouldn't panic
	_, err := executeCommand(cli.RootCmd, "prune")

	// Error is acceptable (not in a hub)
	_ = err
}
