package cmd_test

import (
	"strings"
	"testing"

	"hop.top/git/cmd"
	"hop.top/git/internal/cli"
)

func TestRepairCommand_Help(t *testing.T) {
	out, err := cmd.ExecuteCommand(cli.RootCmd, "repair", "--help")
	if err != nil {
		t.Errorf("Help should not error, got: %v", err)
	}
	for _, want := range []string{"repair", "Usage:", "pathspec", "--undo"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected help to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRepairCommand_Structure(t *testing.T) {
	repairCmd, _, err := cli.RootCmd.Find([]string{"repair"})
	if err != nil {
		t.Fatalf("repair command not found: %v", err)
	}
	if !strings.HasPrefix(repairCmd.Use, "repair") {
		t.Errorf("expected Use to start with 'repair', got %q", repairCmd.Use)
	}
	if repairCmd.Short == "" {
		t.Error("repair must have a Short description")
	}
	if repairCmd.HasSubCommands() {
		t.Error("repair must be a leaf command")
	}
}

func TestRepairCommand_FlagsRegistered(t *testing.T) {
	repairCmd, _, err := cli.RootCmd.Find([]string{"repair"})
	if err != nil {
		t.Fatalf("repair command not found: %v", err)
	}
	for _, name := range []string{"undo", "list-backups", "no-backup", "force-dirty", "progress", "no-progress", "color"} {
		if repairCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected --%s on repair", name)
		}
	}
}

func TestRepairCommand_InheritsGlobalFlags(t *testing.T) {
	repairCmd, _, err := cli.RootCmd.Find([]string{"repair"})
	if err != nil {
		t.Fatalf("repair command not found: %v", err)
	}
	for _, name := range []string{"verbose", "quiet", "json", "porcelain", "dry-run", "force"} {
		flag := repairCmd.Flags().Lookup(name)
		if flag == nil {
			flag = repairCmd.InheritedFlags().Lookup(name)
		}
		if flag == nil {
			t.Errorf("repair should have access to --%s flag", name)
		}
	}
}

func TestRepairCommand_DoctorAliasNoLongerCollides(t *testing.T) {
	doctorCmd, _, err := cli.RootCmd.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("doctor command not found: %v", err)
	}
	for _, alias := range doctorCmd.Aliases {
		if alias == "repair" {
			t.Errorf("doctor must no longer alias 'repair' (collides with new RepairCmd)")
		}
	}
}
