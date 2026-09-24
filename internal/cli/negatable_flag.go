package cli

import "github.com/spf13/cobra"

// NegatableFlag reads a --<name> / --no-<name> pair: nil when neither was
// given on the command line, so config decides; --no-<name> wins.
func NegatableFlag(cmd *cobra.Command, name string, yes, no bool) *bool {
	if cmd == nil {
		return nil
	}
	if cmd.Flags().Changed("no-"+name) && no {
		off := false
		return &off
	}
	if cmd.Flags().Changed(name) {
		v := yes
		return &v
	}
	return nil
}
