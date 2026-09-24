package cli

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/cobra"
)

func TestExitCode(t *testing.T) {
	usage := &UsageError{Err: errors.New("unknown flag: --x")}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"operation failure", errors.New("boom"), 1},
		{"usage error", usage, 129},
		{"wrapped usage error", fmt.Errorf("run: %w", usage), 129},
	}
	for _, tc := range cases {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("%s: ExitCode = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// usageTree is root -> {group -> leaf, runnable-group -> leaf, noargs}.
func usageTree() *cobra.Command {
	run := func(*cobra.Command, []string) {}
	root := &cobra.Command{Use: "tool", Args: cobra.ArbitraryArgs, Run: run}
	root.PersistentFlags().String("config", "", "")
	group := &cobra.Command{Use: "group"}
	group.AddCommand(&cobra.Command{Use: "leaf", Run: run})
	runnableGroup := &cobra.Command{Use: "rgroup", Run: run}
	runnableGroup.AddCommand(&cobra.Command{Use: "leaf", Run: run})
	noargs := &cobra.Command{Use: "noargs", Args: cobra.NoArgs, Run: run}
	root.AddCommand(group, runnableGroup, noargs)
	return root
}

func TestCheckUnknownSubcommand(t *testing.T) {
	cases := []struct {
		args    []string
		refused bool
	}{
		{[]string{"group", "bogus"}, true},
		{[]string{"rgroup", "bogus"}, true},
		{[]string{"group", "--config", "x", "bogus"}, true},
		{[]string{"group"}, false},
		{[]string{"group", "leaf"}, false},
		{[]string{"group", "--config", "x"}, false},
		{[]string{"group", "--help"}, false},
		{[]string{"group", "-h", "bogus"}, false},
		{[]string{"some-branch"}, false},
		{[]string{"noargs", "extra"}, false}, // its own Args validator reports it
	}
	for _, tc := range cases {
		err := checkUnknownSubcommand(usageTree(), tc.args)
		var ue *UsageError
		if got := errors.As(err, &ue); got != tc.refused {
			t.Errorf("%v: refused = %v (err %v), want %v", tc.args, got, err, tc.refused)
		}
	}
}

func TestInstallUsageErrors_ClassifiesCobraValidation(t *testing.T) {
	cases := [][]string{
		{"--bogus"},
		{"noargs", "-Z"},
		{"noargs", "extra"},
	}
	for _, args := range cases {
		root := usageTree()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		installUsageErrors(root)
		installUsageErrors(root) // idempotent
		root.SetArgs(args)
		err := root.Execute()
		if ExitCode(err) != 129 {
			t.Errorf("%v: ExitCode = %d (err %v), want 129", args, ExitCode(err), err)
		}
	}
}

// synopsisTree is root -> {show [<name>] with -a/--all, plain, grp ->
// {one, two, hidden}}.
func synopsisTree() (root, show, plain, grp *cobra.Command) {
	run := func(*cobra.Command, []string) {}
	root = &cobra.Command{Use: "git-hop", Args: cobra.ArbitraryArgs, Run: run}
	root.PersistentFlags().String("config", "", "config file")
	root.Flags().String("branch", "", "branch name")
	show = &cobra.Command{Use: "show [<name>]", Run: run}
	show.Flags().BoolP("all", "a", false, "show all")
	plain = &cobra.Command{Use: "plain", Run: run}
	grp = &cobra.Command{Use: "grp"}
	one := &cobra.Command{Use: "one <x>", Run: run}
	one.Flags().Bool("force", false, "force it")
	grp.AddCommand(one, &cobra.Command{Use: "two", Run: run},
		&cobra.Command{Use: "hidden", Hidden: true, Run: run})
	root.AddCommand(show, plain, grp)
	for _, c := range []*cobra.Command{root, show, plain, grp} {
		c.InitDefaultHelpFlag()
	}
	return root, show, plain, grp
}

func TestUsageBlock(t *testing.T) {
	root, show, plain, grp := synopsisTree()
	cases := []struct {
		name string
		cmd  *cobra.Command
		want string
	}{
		{"leaf with options", show,
			"usage: git hop show [<options>] [<name>]\n\n" +
				"    -a, --all   show all\n\n"},
		{"leaf without options", plain,
			"usage: git hop plain\n\n"},
		{"group lists its visible commands", grp,
			"usage: git hop grp one [<options>] <x>\n" +
				"   or: git hop grp two\n\n"},
		{"root", root,
			"usage: git hop [<options>] <command> [<args>]\n" +
				"   or: git hop [<options>] <branch>\n" +
				"   or: git hop [<options>] <uri> [<path>]\n\n" +
				"        --branch string   branch name\n" +
				"        --config string   config file\n\n"},
	}
	for _, tc := range cases {
		if got := usageBlock(tc.cmd); got != tc.want {
			t.Errorf("%s:\n--- got ---\n%s--- want ---\n%s", tc.name, got, tc.want)
		}
	}
}

func TestStructuredOutputRequested(t *testing.T) {
	root, show, _, _ := synopsisTree()
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("porcelain", false, "")
	root.PersistentFlags().String("format", "table", "")
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"show", "--json", "--bogus"}, true},
		{[]string{"show", "--bogus", "--json"}, true},
		{[]string{"show", "--porcelain", "a", "b"}, true},
		{[]string{"show", "--format=json"}, true},
		{[]string{"show", "--format", "yaml", "-Z"}, true},
		{[]string{"show", "--format=table"}, false},
		{[]string{"show", "--format=human"}, false},
		{[]string{"show", "--json=false"}, false},
		{[]string{"show", "--bogus"}, false},
		{[]string{"show", "--", "--json"}, false},
	}
	for _, tc := range cases {
		if got := structuredOutputRequested(show, tc.args); got != tc.want {
			t.Errorf("%v: structured = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestUsageErrorCarriesCommand(t *testing.T) {
	root, show, plain, grp := synopsisTree()
	plain.Args = cobra.NoArgs
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	installUsageErrors(root)
	cases := []struct {
		args []string
		want *cobra.Command
	}{
		{[]string{"show", "--bogus"}, show},
		{[]string{"plain", "extra"}, plain},
		{[]string{"--bogus"}, root},
	}
	for _, tc := range cases {
		root.SetArgs(tc.args)
		err := root.Execute()
		var ue *UsageError
		if !errors.As(err, &ue) || ue.Cmd != tc.want {
			t.Errorf("%v: UsageError.Cmd = %v (err %v), want %s", tc.args, ue, err, tc.want.Name())
		}
	}
	err := checkUnknownSubcommand(root, []string{"grp", "bogus"})
	var ue *UsageError
	if !errors.As(err, &ue) || ue.Cmd != grp {
		t.Errorf("grp bogus: UsageError = %v, want Cmd grp", err)
	}
}
