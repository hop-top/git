package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// GroupManagement is kit's built-in MANAGEMENT command group: diagnosing
// and repairing hubs and state. Default help leaves it out; --help-all
// adds it, and --help-management or `help management` shows it alone.
// Every other listed command is in kit's default group (no GroupID).
const GroupManagement = "management"

// applyGroupVisibility hides the commands of kit's hidden groups from the
// help this run prints, or shows only one group, as args ask: --help-all,
// --help-<group>, `help <group>` (see kit's Root.ApplyGroupVisibility;
// kit's Root.Execute, which git-hop does not use, calls it itself). A
// shell completion request is left alone: a command hidden from help
// still completes.
//
// Kit's pass also points the help command at whatever it makes of the
// words after `help`, flags included, so `help add --format yaml` became
// an unknown topic; git-hop's own resolution is put back.
func applyGroupVisibility(args []string) {
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		return
	}
	Root.ApplyGroupVisibility()
	if help := helpCommand(RootCmd); help != nil {
		help.RunE = helpRunE(RootCmd)
	}
}

// installHelpCommand makes `git hop help [<command>...]` print the help
// of the command the words name, as `git hop <command> --help` does.
//
// kit registers a hidden `help` subcommand with no RunE, taking exactly
// one word, and resolves that word outside the command tree, in its args
// pass (kit's Root.Execute or Root.ApplyGroupVisibility); dispatched
// through cobra directly it printed its own summary. Installed on the tree
// itself, help works however the tree is run, bare `help` included. The
// resolution here is cobra's Find, so aliases and multi-word paths behave
// as on the command line. A path counts only when every word matched: a
// partial match is an unknown topic, not help for the parent.
func installHelpCommand(root *cobra.Command) {
	help := helpCommand(root)
	if help == nil {
		return
	}
	help.Use = "help [<command>...]"
	help.Args = func(_ *cobra.Command, args []string) error {
		if len(args) == 0 || helpTarget(root, args) != nil {
			return nil
		}
		return fmt.Errorf("unknown help topic %q for %q", strings.Join(args, " "), root.Name())
	}
	help.RunE = helpRunE(root)
}

// helpCommand returns root's help subcommand, or nil.
func helpCommand(root *cobra.Command) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == "help" {
			return c
		}
	}
	return nil
}

// helpRunE prints the help of the command the words name, or root's.
func helpRunE(root *cobra.Command) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		target := root
		if len(args) > 0 {
			target = helpTarget(root, args)
		}
		// cobra adds these flags to a command only when it is the one
		// dispatched; add them so the rendered flag list matches --help.
		target.InitDefaultHelpFlag()
		target.InitDefaultVersionFlag()
		return target.Help()
	}
}

// helpTarget returns the command args name in full, or nil.
func helpTarget(root *cobra.Command, args []string) *cobra.Command {
	target, rest, err := root.Find(args)
	if err != nil || target == nil || target == root || len(rest) > 0 {
		return nil
	}
	return target
}
