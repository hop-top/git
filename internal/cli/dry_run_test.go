package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newDryRunTree builds a root with the global persistent --dry-run and
// one leaf per case the guard distinguishes.
func newDryRunTree() (root, marked, unmarked, local, help *cobra.Command) {
	noop := func(*cobra.Command, []string) {}
	root = &cobra.Command{Use: "git-hop", Run: noop}
	root.PersistentFlags().Bool("dry-run", false, "")

	marked = &cobra.Command{Use: "remove", Run: noop}
	SupportDryRun(marked)

	unmarked = &cobra.Command{Use: "start", Run: noop}
	env := &cobra.Command{Use: "env"}
	env.AddCommand(unmarked)

	local = &cobra.Command{Use: "repair", Run: noop}
	local.Flags().Bool("dry-run", false, "")

	help = &cobra.Command{Use: "help", Run: noop}

	root.AddCommand(marked, env, local, help)
	return root, marked, unmarked, local, help
}

func parse(t *testing.T, cmd *cobra.Command, args ...string) {
	t.Helper()
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
}

func TestCheckDryRunSupported(t *testing.T) {
	t.Run("unmarked command refuses the global flag", func(t *testing.T) {
		_, _, unmarked, _, _ := newDryRunTree()
		parse(t, unmarked, "--dry-run")
		err := checkDryRunSupported(unmarked)
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if want := "'git hop env start'"; !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	})

	t.Run("marked command accepts the global flag", func(t *testing.T) {
		_, marked, _, _, _ := newDryRunTree()
		parse(t, marked, "--dry-run")
		if err := checkDryRunSupported(marked); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})

	t.Run("marked root accepts the global flag", func(t *testing.T) {
		root, _, _, _, _ := newDryRunTree()
		SupportDryRun(root)
		parse(t, root, "--dry-run")
		if err := checkDryRunSupported(root); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})

	t.Run("unset flag is not a dry run", func(t *testing.T) {
		_, _, unmarked, _, _ := newDryRunTree()
		parse(t, unmarked)
		if err := checkDryRunSupported(unmarked); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})

	t.Run("explicit false is not a dry run", func(t *testing.T) {
		_, _, unmarked, _, _ := newDryRunTree()
		parse(t, unmarked, "--dry-run=false")
		if err := checkDryRunSupported(unmarked); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})

	t.Run("local flag shadows the global one", func(t *testing.T) {
		root, _, _, local, _ := newDryRunTree()
		parse(t, local, "--dry-run")
		if err := checkDryRunSupported(local); err != nil {
			t.Errorf("want nil, got %v", err)
		}
		if v := root.PersistentFlags().Lookup("dry-run").Value.String(); v != "false" {
			t.Errorf("global flag = %s, want false: local flag did not shadow it", v)
		}
	})

	t.Run("help is exempt", func(t *testing.T) {
		_, _, _, _, help := newDryRunTree()
		parse(t, help, "--dry-run")
		if err := checkDryRunSupported(help); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})
}

// TestCheckDryRunSupported_Shorthand runs the guard against the real root
// flags: -n must reach it exactly as --dry-run does.
func TestCheckDryRunSupported_Shorthand(t *testing.T) {
	for _, arg := range []string{"-n", "--dry-run"} {
		t.Run(arg, func(t *testing.T) {
			global := RootCmd.PersistentFlags().Lookup("dry-run")
			t.Cleanup(func() {
				_ = global.Value.Set("false")
				global.Changed = false
			})

			parse(t, upgradePreambleCmd, arg)
			err := checkDryRunSupported(upgradePreambleCmd)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if want := "'git hop upgrade preamble'"; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		})
	}
}
