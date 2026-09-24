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

// requestedOutput reads the output flags given in args to cmd.
//
// A usage error stops cobra's parse at the bad flag, before the output
// mode is set up and possibly before the output flags were even reached,
// so args are read with a probe that skips unknown flags instead.
func requestedOutput(cmd *cobra.Command, args []string) outputRequest {
	probe := probeFlagSet(cmd)
	_ = probe.Parse(args) // a malformed tail leaves the flags read so far
	on := func(name string) bool {
		f := probe.Lookup(name)
		if f == nil {
			return false
		}
		v, err := strconv.ParseBool(f.Value.String())
		return err == nil && v
	}
	req := outputRequest{json: on("json"), porcelain: on("porcelain")}
	if f := probe.Lookup("format"); f != nil {
		req.format = f.Value.String()
		req.formatExplicit = probe.Changed("format")
	}
	return req
}

// structuredOutputRequested reports whether args ask cmd for machine
// output: --json, --porcelain, or a --format other than the human ones.
func structuredOutputRequested(cmd *cobra.Command, args []string) bool {
	req := requestedOutput(cmd, args)
	return req.json || req.porcelain || !output.IsHumanFormat(req.format)
}

// jsonUsageErrorRequested reports whether args put cmd's diagnostics in
// JSON mode, by the rule the pre-run applies, so a usage error reports
// in the shape an operation failure under the same flags would. A
// contradictory request is refused in plain text by the pre-run, and is
// here too.
func jsonUsageErrorRequested(cmd *cobra.Command, args []string) bool {
	req := requestedOutput(cmd, args)
	format, _, err := req.resultFormat(declaresResult(cmd))
	return err == nil && req.mode(format) == output.ModeJSON
}
