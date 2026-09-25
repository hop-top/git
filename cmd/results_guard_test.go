package cmd

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"hop.top/git/internal/cli"
)

// withoutResult are the commands that declare no result and are not
// exempt: they refuse --json, --porcelain and a structured --format
// (exit 129). Each is here by decision, with the reason.
var withoutResult = map[string]string{
	"upgrade":          "installs a new binary; kit's upgrade flow prints prose only",
	"upgrade preamble": "prints (or with --install writes) a markdown fragment, not a result",
}

// commandTree lists every runnable command under root by its path
// without the root's name ("" for the root itself).
func commandTree(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		if c.Runnable() {
			out[path] = c
		}
		for _, sub := range c.Commands() {
			p := sub.Name()
			if path != "" {
				p = path + " " + p
			}
			walk(sub, p)
		}
	}
	walk(root, "")
	return out
}

// Every runnable command declares a result shape, is exempt from one
// (ExemptFromResult: a read-only helper whose output is not a result),
// or is listed in withoutResult. A new command that does none of these
// fails here, so none can silently print nothing under --json.
func TestEveryCommandDeclaresAResultOrIsExempt(t *testing.T) {
	tree := commandTree(cli.RootCmd)
	paths := make([]string, 0, len(tree))
	for p := range tree {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		c := tree[path]
		_, _, declared := kitcli.GetOutputSchemaJSON(c)
		exempt := cli.IsExemptFromResult(c)
		_, listed := withoutResult[path]
		switch {
		case declared && (exempt || listed):
			t.Errorf("'git hop %s' declares a result shape and is also exempt or listed without one", path)
		case exempt && listed:
			t.Errorf("'git hop %s' is both exempt and listed in withoutResult", path)
		case !declared && !exempt && !listed:
			t.Errorf("'git hop %s' declares no result shape: declare one (declareOutputSchema), "+
				"exempt it (cli.ExemptFromResult) if it is a read-only helper with no result, "+
				"or list it in withoutResult", path)
		}
	}
	for path := range withoutResult {
		if _, ok := tree[path]; !ok {
			t.Errorf("withoutResult lists 'git hop %s', which is not a runnable command", path)
		}
	}
}
