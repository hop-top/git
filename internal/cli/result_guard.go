package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"hop.top/git/internal/output"
)

// noResultAnnotation marks a command that has no structured result and
// still accepts the output flags: --json, --porcelain and --format only
// switch its log format.
const noResultAnnotation = "hop.top/git/no-result"

// ExemptFromResult declares that each of cmds legitimately has no
// structured result, so the output flags are accepted without one.
//
// The exemption is opt-in on purpose, like SupportDryRun. The output
// flags are persistent root flags, so every command parses them; a
// command that declares no output schema would print its human view,
// or nothing, while a script waits for a result. checkResultDeclared
// refuses them on any command that neither declares a schema nor is
// exempt, so a new command fails closed instead. Only read-only helpers
// whose output is not a result belong here: shell scripts, shell
// integration plumbing.
func ExemptFromResult(cmds ...*cobra.Command) {
	for _, c := range cmds {
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.Annotations[noResultAnnotation] = "true"
	}
}

// IsExemptFromResult reports whether cmd accepts the output flags
// without a structured result: marked by ExemptFromResult, or one of
// cobra's help and completion plumbing commands, which run no command
// logic.
func IsExemptFromResult(cmd *cobra.Command) bool {
	if _, ok := cmd.Annotations[noResultAnnotation]; ok {
		return true
	}
	switch cmd.Name() {
	case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	return false
}

// checkResultDeclared returns an error when r asks for a structured
// result from cmd, which declares none and is not exempt. A command
// group that runs nothing itself (env) is left alone: it only ever
// prints its help or a usage error, which --json still puts in JSON.
func (r outputRequest) checkResultDeclared(cmd *cobra.Command) error {
	if cmd == nil || !cmd.Runnable() || IsExemptFromResult(cmd) {
		return nil
	}
	var flag string
	switch {
	case r.json:
		flag = "--json"
	case r.porcelain:
		flag = "--porcelain"
	case !output.IsHumanFormat(r.format):
		flag = "--format=" + r.format
	default:
		return nil
	}
	name := strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()))
	return fmt.Errorf("%s is not supported by 'git hop %s': it has no structured result", flag, name)
}
