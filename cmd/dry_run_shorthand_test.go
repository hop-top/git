package cmd_test

import (
	"testing"

	"github.com/spf13/cobra"

	"hop.top/git/internal/cli"
)

// walkCommands visits root and every command below it.
func walkCommands(root *cobra.Command, fn func(*cobra.Command)) {
	fn(root)
	for _, c := range root.Commands() {
		walkCommands(c, fn)
	}
}

// resetDryRun clears whichever --dry-run flag cmd parsed so later tests
// see the tree unparsed.
func resetDryRun(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	f := cmd.Flags().Lookup("dry-run")
	if f == nil {
		return
	}
	if err := f.Value.Set("false"); err != nil {
		t.Fatalf("reset --dry-run: %v", err)
	}
	f.Changed = false
}

// TestDryRunShorthand_EveryCommand builds the full command tree and checks
// that -n means --dry-run on every command in it, git porcelain style.
//
// Merging the root's persistent flags into a command panics when the
// command already binds -n to a flag of another name, so the merge runs
// under recover: a collision fails its subtest instead of the binary.
func TestDryRunShorthand_EveryCommand(t *testing.T) {
	root := cli.RootCmd
	root.InitDefaultHelpCmd()

	walkCommands(root, func(c *cobra.Command) {
		t.Run(c.CommandPath(), func(t *testing.T) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("merging persistent flags panicked: %v", r)
					}
				}()
				_ = c.InheritedFlags()
			}()

			dryRun := c.Flags().Lookup("dry-run")
			if dryRun == nil {
				t.Fatal("--dry-run not defined")
			}
			short := c.Flags().ShorthandLookup("n")
			if short == nil {
				t.Fatal("-n not defined")
			}
			if short != dryRun {
				t.Fatalf("-n is --%s, want --dry-run", short.Name)
			}

			defer resetDryRun(t, c)
			if err := c.ParseFlags([]string{"-n"}); err != nil {
				t.Fatalf("parse -n: %v", err)
			}
			if got, _ := c.Flags().GetBool("dry-run"); !got {
				t.Error("-n did not set --dry-run")
			}
		})
	})
}
