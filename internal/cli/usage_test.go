package cli

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/cobra"
)

func TestExitCode(t *testing.T) {
	usage := &UsageError{Err: errors.New("unknown flag: --x")}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"operation failure", errors.New("boom"), 1},
		{"usage error", usage, 129},
		{"wrapped usage error", fmt.Errorf("run: %w", usage), 129},
	}
	for _, tc := range cases {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("%s: ExitCode = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// usageTree is root -> {group -> leaf, runnable-group -> leaf, noargs}.
func usageTree() *cobra.Command {
	run := func(*cobra.Command, []string) {}
	root := &cobra.Command{Use: "tool", Args: cobra.ArbitraryArgs, Run: run}
	root.PersistentFlags().String("config", "", "")
	group := &cobra.Command{Use: "group"}
	group.AddCommand(&cobra.Command{Use: "leaf", Run: run})
	runnableGroup := &cobra.Command{Use: "rgroup", Run: run}
	runnableGroup.AddCommand(&cobra.Command{Use: "leaf", Run: run})
	noargs := &cobra.Command{Use: "noargs", Args: cobra.NoArgs, Run: run}
	root.AddCommand(group, runnableGroup, noargs)
	return root
}

func TestCheckUnknownSubcommand(t *testing.T) {
	cases := []struct {
		args    []string
		refused bool
	}{
		{[]string{"group", "bogus"}, true},
		{[]string{"rgroup", "bogus"}, true},
		{[]string{"group", "--config", "x", "bogus"}, true},
		{[]string{"group"}, false},
		{[]string{"group", "leaf"}, false},
		{[]string{"group", "--config", "x"}, false},
		{[]string{"group", "--help"}, false},
		{[]string{"group", "-h", "bogus"}, false},
		{[]string{"some-branch"}, false},
		{[]string{"noargs", "extra"}, false}, // its own Args validator reports it
	}
	for _, tc := range cases {
		err := checkUnknownSubcommand(usageTree(), tc.args)
		var ue *UsageError
		if got := errors.As(err, &ue); got != tc.refused {
			t.Errorf("%v: refused = %v (err %v), want %v", tc.args, got, err, tc.refused)
		}
	}
}

func TestInstallUsageErrors_ClassifiesCobraValidation(t *testing.T) {
	cases := [][]string{
		{"--bogus"},
		{"noargs", "-Z"},
		{"noargs", "extra"},
	}
	for _, args := range cases {
		root := usageTree()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		installUsageErrors(root)
		installUsageErrors(root) // idempotent
		root.SetArgs(args)
		err := root.Execute()
		if ExitCode(err) != 129 {
			t.Errorf("%v: ExitCode = %d (err %v), want 129", args, ExitCode(err), err)
		}
	}
}
