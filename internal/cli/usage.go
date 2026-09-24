package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// UsageError is a command line the CLI refused before any command logic
// ran: an unknown or malformed flag, a wrong number of positional
// arguments, or an unknown subcommand. Git reports these with status 129.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// ExitCode maps an error returned by Execute to the process status:
// 129 for a usage error, 1 for any other failure.
func ExitCode(err error) int {
	var ue *UsageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		return exitUsage
	default:
		return 1
	}
}

// usageAnnotation marks a command whose Args validator already reports
// usage errors, so installUsageErrors wraps it once.
const usageAnnotation = "hop.top/git/usage-args"

// installUsageErrors classifies cobra's own validation of the invocation
// as usage errors. Cobra raises these before the command runs, at two
// seams: the FlagErrorFunc (pflag parse failures, inherited from the
// root) and each command's Args validator. Both are typed by where they
// come from, so no message matching is needed.
//
// Run it after every command is registered; it is idempotent.
func installUsageErrors(root *cobra.Command) {
	root.SetFlagErrorFunc(asUsageError)
	walkCommands(root, func(c *cobra.Command) {
		if c.Args == nil || c.Annotations[usageAnnotation] != "" {
			return
		}
		orig := c.Args
		c.Args = func(c *cobra.Command, args []string) error {
			return asUsageError(c, orig(c, args))
		}
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.Annotations[usageAnnotation] = "true"
	})
}

// asUsageError wraps err as a UsageError and silences cobra's own
// "Error:" line for it; Execute prints the git-style line instead.
func asUsageError(c *cobra.Command, err error) error {
	if err == nil || errors.Is(err, pflag.ErrHelp) {
		return err
	}
	c.SilenceErrors = true
	return &UsageError{Err: err}
}

// checkUnknownSubcommand refuses a word that names no subcommand of a
// command group, e.g. `git hop env bogus`.
//
// Cobra accepts such a word silently: a group that is not runnable is
// handed to its help renderer (exit 0) before any Args validator runs,
// and a runnable group without an Args validator ignores it. The root is
// exempt: its positional is a branch or repository, not a subcommand.
func checkUnknownSubcommand(root *cobra.Command, args []string) error {
	target, rest, err := root.Find(args)
	if err != nil || target == root || !target.HasSubCommands() || target.Args != nil {
		return nil
	}
	words, err := positionalWords(target, rest)
	if err != nil || len(words) == 0 {
		// A malformed flag, including a help request, is left to cobra.
		return nil
	}
	return &UsageError{Err: fmt.Errorf("unknown command %q for %q", words[0], target.CommandPath())}
}

// positionalWords returns the non-flag words of args as cobra would
// parse them for cmd. It parses against placeholder values so the real
// flags are untouched for the parse cobra does next. The help flag is
// left undefined, so a help request surfaces as pflag.ErrHelp.
func positionalWords(cmd *cobra.Command, args []string) ([]string, error) {
	probe := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	probe.SetOutput(io.Discard)
	probe.ParseErrorsAllowlist.UnknownFlags = true
	add := func(f *pflag.Flag) {
		if f.Name == "help" || probe.Lookup(f.Name) != nil {
			return
		}
		shorthand := f.Shorthand
		if shorthand == "h" || (shorthand != "" && probe.ShorthandLookup(shorthand) != nil) {
			shorthand = ""
		}
		probe.AddFlag(&pflag.Flag{
			Name:        f.Name,
			Shorthand:   shorthand,
			Value:       placeholder{typ: f.Value.Type()},
			NoOptDefVal: f.NoOptDefVal,
		})
	}
	cmd.LocalFlags().VisitAll(add)
	cmd.InheritedFlags().VisitAll(add)
	if err := probe.Parse(args); err != nil {
		return nil, err
	}
	return probe.Args(), nil
}

// placeholder accepts any flag value and stores nothing.
type placeholder struct{ typ string }

func (p placeholder) String() string   { return "" }
func (p placeholder) Set(string) error { return nil }
func (p placeholder) Type() string     { return p.typ }

func walkCommands(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, sub := range c.Commands() {
		walkCommands(sub, fn)
	}
}
