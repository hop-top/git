package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// installHelpCommand makes `git hop help [<command>...]` print the help
// of the command the words name, as `git hop <command> --help` does.
//
// kit registers a hidden `help` subcommand with no RunE and resolves its
// operand only inside kit's Root.Execute, which git-hop does not use;
// dispatched through cobra directly it printed its own summary. The
// resolution here is cobra's Find, so aliases and multi-word paths behave
// as on the command line. A path counts only when every word matched: a
// partial match is an unknown topic, not help for the parent.
func installHelpCommand(root *cobra.Command) {
	var help *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "help" {
			help = c
			break
		}
	}
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
	help.RunE = func(_ *cobra.Command, args []string) error {
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
