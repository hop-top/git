package cli_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"

	_ "hop.top/git/cmd" // registers every subcommand on cli.RootCmd
	"hop.top/git/internal/cli"
)

// unboundedArity lists the runnable leaves that legitimately take any
// number of positionals. Every other leaf must refuse one word past its
// arity as a usage error. Keep each entry justified.
var unboundedArity = map[string]string{
	"git-hop repair": "takes a pathspec list, like git add -- <pathspec>...",
}

// arityProbeLimit is how many positionals the probe tries before calling
// a command unbounded.
const arityProbeLimit = 8

// TestLeafCommandsRefuseSurplusPositionals walks the real command tree
// and checks that each runnable leaf declares its arity: given one
// positional more than it accepts, it refuses with usage status 129.
//
// A leaf without an Args validator accepts any number of words and
// silently ignores them (`git hop list extra` exited 0). Only the Args
// validator is invoked, never the command itself, so nothing touches
// the filesystem.
func TestLeafCommandsRefuseSurplusPositionals(t *testing.T) {
	cli.InstallUsageErrors(cli.RootCmd)

	seen := map[string]bool{}
	walk(cli.RootCmd, func(c *cobra.Command) {
		if c == cli.RootCmd || c.HasSubCommands() || !c.Runnable() {
			return
		}
		path := c.CommandPath()
		seen[path] = true

		most, bounded := acceptedArity(c)
		if _, ok := unboundedArity[path]; ok {
			if bounded {
				t.Errorf("%s: listed in unboundedArity but accepts at most %d positional(s); drop the entry", path, most)
			}
			return
		}
		if !bounded {
			t.Errorf("%s: accepts %d+ positionals; declare its arity with an Args validator", path, arityProbeLimit)
			return
		}
		if most < 0 {
			t.Errorf("%s: accepts no positional count in 0..%d; the probe cannot find its arity", path, arityProbeLimit)
			return
		}
		err := c.ValidateArgs(fill(c, most+1))
		var ue *cli.UsageError
		if !errors.As(err, &ue) || cli.ExitCode(err) != 129 {
			t.Errorf("%s: %d positional(s) gave %v (exit %d), want a usage error (exit 129)",
				path, most+1, err, cli.ExitCode(err))
		}
	})

	for path := range unboundedArity {
		if !seen[path] {
			t.Errorf("unboundedArity lists %q, which is not a runnable leaf", path)
		}
	}
}

// acceptedArity returns the largest positional count c accepts up to
// arityProbeLimit (-1 when it accepts none), and whether c refuses
// arityProbeLimit words, i.e. is bounded.
func acceptedArity(c *cobra.Command) (most int, bounded bool) {
	most = -1
	for n := 0; n <= arityProbeLimit; n++ {
		if c.ValidateArgs(fill(c, n)) == nil {
			most = n
		}
	}
	return most, most < arityProbeLimit
}

// fill builds n positionals c would accept by content, so only their
// count is under test.
func fill(c *cobra.Command, n int) []string {
	args := make([]string, n)
	for i := range args {
		if len(c.ValidArgs) > 0 {
			args[i] = c.ValidArgs[0]
		} else {
			args[i] = fmt.Sprintf("word%d", i)
		}
	}
	return args
}

func walk(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, sub := range c.Commands() {
		walk(sub, fn)
	}
}
