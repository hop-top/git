package cmd

import (
	"testing"

	"github.com/spf13/pflag"
)

// initTestFlags declares the conversion flags as init does, for parsing
// a command line without touching initCmd's own flag state.
func initTestFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("init", pflag.ContinueOnError)
	var b bool
	var s string
	fs.BoolVarP(&b, "dry-run", "n", false, "")
	fs.Bool("no-prompt", false, "")
	fs.Bool("regular", false, "")
	fs.Bool("force", false, "")
	fs.Bool("keep-backup", false, "")
	fs.Bool("no-hooks", false, "")
	fs.Bool("enable-chdir", false, "")
	fs.StringVar(&s, "hooks", "", "")
	fs.Bool("hooks-overwrite", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs
}

// TestInitProceedCommand pins the dry run's closing hint: it names the
// flags the user gave that shape the conversion, so running it performs
// the conversion just previewed; -n itself is dropped.
func TestInitProceedCommand(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-n"}, "git hop init"},
		{[]string{"-n", "--no-prompt"}, "git hop init --no-prompt"},
		{[]string{"-n", "--no-prompt", "--regular"}, "git hop init --no-prompt --regular"},
		{[]string{"--regular", "--dry-run", "--no-prompt"}, "git hop init --no-prompt --regular"},
		{[]string{"-n", "--no-prompt", "--force", "--keep-backup=false", "--no-hooks"},
			"git hop init --no-prompt --force --keep-backup=false --no-hooks"},
		{[]string{"-n", "--hooks", "copy", "--hooks-overwrite", "--enable-chdir"},
			"git hop init --hooks=copy --hooks-overwrite --enable-chdir"},
	}
	for _, tc := range cases {
		if got := initProceedCommand(initTestFlags(t, tc.args...)); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.args, got, tc.want)
		}
	}
	if got := initProceedCommand(nil); got != "git hop init" {
		t.Errorf("no flag set: got %q", got)
	}
}

// Every flag the hint may echo is one init really declares.
func TestInitProceedFlags_AreDeclared(t *testing.T) {
	for _, name := range initProceedFlags {
		if initCmd.Flags().Lookup(name) == nil {
			t.Errorf("hint echoes --%s, which init does not declare", name)
		}
	}
}
