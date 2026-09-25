package cmd

import "hop.top/git/internal/cli"

// The root command (`git hop <arg>`: branch switch, clone, fork-attach)
// renders cli.RootResult.
func init() {
	declareOutputSchema(cli.RootCmd, &cli.RootResult{})
}
