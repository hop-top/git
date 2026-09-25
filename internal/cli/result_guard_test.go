package cli

import (
	"testing"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// newResultTree builds a root with one leaf per case the guard
// distinguishes: a command with a result, one without (in a group that
// runs nothing itself), one exempt, and cobra's help and completion
// plumbing.
func newResultTree(t *testing.T) (declared, undeclared, exempt *cobra.Command, plumbing []*cobra.Command) {
	t.Helper()
	noop := func(*cobra.Command, []string) {}
	root := &cobra.Command{Use: "git-hop", Run: noop}
	declared = &cobra.Command{Use: "add", Run: noop}
	type record struct {
		Name string `json:"name"`
	}
	if err := kitcli.SetOutputSchema(declared, kitcli.OutputSchema{Type: &record{}, Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	undeclared = &cobra.Command{Use: "start", Run: noop}
	env := &cobra.Command{Use: "env"}
	env.AddCommand(undeclared)
	exempt = &cobra.Command{Use: "completion", Run: noop}
	ExemptFromResult(exempt)
	plumbing = []*cobra.Command{
		{Use: "help", Run: noop},
		{Use: cobra.ShellCompRequestCmd, Run: noop},
		{Use: cobra.ShellCompNoDescRequestCmd, Run: noop},
	}
	root.AddCommand(declared, env, exempt)
	root.AddCommand(plumbing...)
	return declared, undeclared, exempt, plumbing
}

func TestResultFormat_RefusesStructuredModesWithoutAResult(t *testing.T) {
	declared, undeclared, exempt, plumbing := newResultTree(t)

	structured := map[string]outputRequest{
		"--json":        {json: true, format: "table"},
		"--porcelain":   {porcelain: true, format: "table"},
		"--format=json": {format: "json", formatExplicit: true},
		"--format=yaml": {format: "yaml", formatExplicit: true},
		"--format=csv":  {format: "csv", formatExplicit: true},
	}
	for flag, req := range structured {
		t.Run("undeclared refuses "+flag, func(t *testing.T) {
			_, _, err := req.resultFormat(undeclared)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			want := flag + " is not supported by 'git hop env start': it has no structured result"
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
		t.Run("declared accepts "+flag, func(t *testing.T) {
			format, _, err := req.resultFormat(declared)
			if err != nil || format == "" {
				t.Errorf("format = %q, err = %v; want a structured format", format, err)
			}
		})
		for _, c := range append([]*cobra.Command{exempt, undeclared.Parent()}, plumbing...) {
			t.Run(c.Name()+" accepts "+flag, func(t *testing.T) {
				format, _, err := req.resultFormat(c)
				if err != nil || format != "" {
					t.Errorf("format = %q, err = %v; want the human view", format, err)
				}
			})
		}
	}

	human := map[string]outputRequest{
		"no flag":        {format: "table"},
		"--format=table": {format: "table", formatExplicit: true},
		"--format=human": {format: "human", formatExplicit: true},
		"-q":             {quiet: true, format: "table"},
	}
	for name, req := range human {
		t.Run("undeclared accepts "+name, func(t *testing.T) {
			format, _, err := req.resultFormat(undeclared)
			if err != nil || format != "" {
				t.Errorf("format = %q, err = %v; want the human view", format, err)
			}
		})
	}
}

func TestExemptFromResult_KeepsExistingAnnotations(t *testing.T) {
	c := &cobra.Command{Use: "x", Annotations: map[string]string{"other": "1"}}
	ExemptFromResult(c)
	if !IsExemptFromResult(c) || c.Annotations["other"] != "1" {
		t.Errorf("annotations = %v", c.Annotations)
	}
	if IsExemptFromResult(&cobra.Command{Use: "y"}) {
		t.Error("an unmarked command is not exempt")
	}
}
