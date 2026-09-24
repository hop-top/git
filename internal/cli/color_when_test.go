package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"hop.top/git/internal/output"
)

// A command's own --color=<when> decides the colour of its run; the
// global --no-color still turns colour off whatever it says.
func TestColorWhenFollowsCommandFlag(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		noColor bool
		want    output.ColorWhen
	}{
		{"flag not given", nil, false, output.ColorAuto},
		{"--color=always", []string{"--color=always"}, false, output.ColorAlways},
		{"bare --color", []string{"--color"}, false, output.ColorAlways},
		{"--color=never", []string{"--color=never"}, false, output.ColorNever},
		{"--no-color beats --color=always", []string{"--color=always"}, true, output.ColorNever},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			when := output.ColorAuto
			cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
			cmd.Flags().Var(&when, "color", "")
			cmd.Flags().Lookup("color").NoOptDefVal = string(output.ColorAlways)
			if err := cmd.Flags().Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			if got := colorWhen(cmd, tc.noColor); got != tc.want {
				t.Errorf("colorWhen = %q, want %q", got, tc.want)
			}
		})
	}

	plain := &cobra.Command{Use: "y"}
	if got := colorWhen(plain, false); got != output.ColorAuto {
		t.Errorf("command without --color: colorWhen = %q, want auto", got)
	}
	if got := colorWhen(plain, true); got != output.ColorNever {
		t.Errorf("command without --color, --no-color: colorWhen = %q, want never", got)
	}
}
