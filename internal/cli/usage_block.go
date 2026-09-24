package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"hop.top/git/internal/output"
)

// rootSynopsis is the top-level usage: the root takes a subcommand, or a
// branch to switch to, or a repository to clone.
var rootSynopsis = []string{
	"git hop [<options>] <command> [<args>]",
	"git hop [<options>] <branch>",
	"git hop [<options>] <uri> [<path>]",
}

// usageBlock renders c's usage the way git prints it after a usage
// error: the synopsis, `usage:` then `   or:` per alternative form, a
// blank line, then c's own options, each block closed by a blank line.
// A command group lists the synopsis of every command under it.
//
// Only c's own options are listed, as git lists only the subcommand's;
// global options are one `--help` away.
func usageBlock(c *cobra.Command) string {
	var b strings.Builder
	for i, line := range usageSynopsis(c) {
		if i == 0 {
			b.WriteString("usage: ")
		} else {
			b.WriteString("   or: ")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	if opts := ownOptions(c); opts.HasFlags() {
		for _, line := range strings.SplitAfter(opts.FlagUsages(), "\n") {
			if line != "" {
				b.WriteString("  " + line)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func usageSynopsis(c *cobra.Command) []string {
	if c == c.Root() {
		return rootSynopsis
	}
	if !c.HasSubCommands() {
		return []string{synopsis(c)}
	}
	var lines []string
	if c.Runnable() {
		lines = append(lines, synopsis(c))
	}
	for _, sub := range c.Commands() {
		if sub.IsAvailableCommand() {
			lines = append(lines, usageSynopsis(sub)...)
		}
	}
	return lines
}

// synopsis is one usage line for c: its path as typed under git, an
// options marker when it has options, then the operands its Use names.
func synopsis(c *cobra.Command) string {
	parts := []string{"git hop" + strings.TrimPrefix(c.CommandPath(), c.Root().Name())}
	if ownOptions(c).HasFlags() {
		parts = append(parts, "[<options>]")
	}
	if _, operands, ok := strings.Cut(c.Use, " "); ok {
		parts = append(parts, strings.TrimSpace(operands))
	}
	return strings.Join(parts, " ")
}

// ownOptions is the visible flags c defines itself, help excluded.
func ownOptions(c *cobra.Command) *pflag.FlagSet {
	fs := pflag.NewFlagSet(c.Name(), pflag.ContinueOnError)
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			fs.AddFlag(f)
		}
	})
	return fs
}

// structuredOutputRequested reports whether args ask cmd for machine
// output: --json, --porcelain, or a --format other than the human ones.
//
// A usage error stops cobra's parse at the bad flag, before the output
// mode is set up and possibly before the output flags were even reached,
// so args are read with a probe that skips unknown flags instead.
func structuredOutputRequested(cmd *cobra.Command, args []string) bool {
	probe := probeFlagSet(cmd)
	_ = probe.Parse(args) // a malformed tail leaves the flags read so far
	given := func(name string) string {
		if f := probe.Lookup(name); f != nil {
			return f.Value.String()
		}
		return ""
	}
	on := func(name string) bool {
		v, err := strconv.ParseBool(given(name))
		return err == nil && v
	}
	return on("json") || on("porcelain") || !output.IsHumanFormat(given("format"))
}
