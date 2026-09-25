package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/pflag"
)

// initHintFlagOrder is the order in which init's hints name flags.
var initHintFlagOrder = []string{
	"restore", "no-prompt", "regular", "force", "keep-backup",
	"no-hooks", "hooks", "hooks-overwrite", "enable-chdir", "dry-run",
}

// initProceedFlags are the init flags that shape a conversion. A hint
// offering a conversion repeats those the user gave.
var initProceedFlags = []string{
	"no-prompt", "regular", "force", "keep-backup",
	"no-hooks", "hooks", "hooks-overwrite", "enable-chdir",
}

// initRetryFlags are what a refusal's "run it again" repeats: the
// command as run, the dry run included.
var initRetryFlags = append(slices.Clone(initProceedFlags), "dry-run")

// initArg is a flag a hint's command sets itself; value is empty for a
// boolean flag.
type initArg struct{ name, value string }

// initCommandLine is the `git hop init` command line a hint offers: the
// flags in echo the user set, as given, and the flags in set, in
// initHintFlagOrder. A flag in set replaces the user's own, so it is
// never named twice. Every init hint that suggests running init builds
// its command here, so none can drop the flags the user gave.
func initCommandLine(flags *pflag.FlagSet, echo []string, set ...initArg) string {
	parts := []string{"git hop init"}
	for _, name := range initHintFlagOrder {
		if i := slices.IndexFunc(set, func(a initArg) bool { return a.name == name }); i >= 0 {
			parts = append(parts, "--"+name)
			if set[i].value != "" {
				parts = append(parts, set[i].value)
			}
			continue
		}
		if flags == nil || !slices.Contains(echo, name) {
			continue
		}
		f := flags.Lookup(name)
		if f == nil || !f.Changed {
			continue
		}
		if f.Value.Type() == "bool" && f.Value.String() == "true" {
			parts = append(parts, "--"+name)
			continue
		}
		parts = append(parts, fmt.Sprintf("--%s=%s", name, f.Value.String()))
	}
	return strings.Join(parts, " ")
}

// initProceedCommand runs the conversion a dry run previewed, or the one
// a dirty tree stopped once it is clean. -n is dropped.
func initProceedCommand(flags *pflag.FlagSet) string {
	return initCommandLine(flags, initProceedFlags)
}

// initForceCommand runs the conversion a dirty tree stopped, carrying
// the uncommitted changes.
func initForceCommand(flags *pflag.FlagSet) string {
	return initCommandLine(flags, initProceedFlags, initArg{name: "force"})
}

// initRetryCommand repeats the command a detached HEAD or an operation
// in progress refused, once that is dealt with. -n is kept: the retry is
// the same command, not a conversion the user never asked for. --force
// is kept too: it does not lift these refusals, but it is the user's
// consent for the conversion being retried.
func initRetryCommand(flags *pflag.FlagSet) string {
	return initCommandLine(flags, initRetryFlags)
}

// initRegularCommand converts to the regular layout, which linked
// worktrees do not stop. --regular only selects it with --no-prompt, so
// both are set.
func initRegularCommand(flags *pflag.FlagSet) string {
	return initCommandLine(flags, initProceedFlags,
		initArg{name: "no-prompt"}, initArg{name: "regular"})
}

// initConvertCommand converts a repository registered as-is, keeping the
// hook and shell choices made for the registration.
func initConvertCommand(flags *pflag.FlagSet) string {
	return initCommandLine(flags, initProceedFlags, initArg{name: "no-prompt"})
}

// initRestoreCommand restores backupPath to its original location; with
// force, over whatever occupies it. No conversion flag applies to a
// restore.
func initRestoreCommand(backupPath string, force bool) string {
	set := []initArg{{name: "restore", value: backupPath}}
	if force {
		set = append(set, initArg{name: "force"})
	}
	return initCommandLine(nil, nil, set...)
}
