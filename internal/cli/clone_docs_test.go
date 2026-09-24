package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// cloneDocHeading opens the reference section for clone mode, `git hop <uri>`.
const cloneDocHeading = "### git hop `&lt;uri&gt;`"

// porcelainExitCodes are the statuses git-hop exits with, per the git
// porcelain convention: success, operation failure, fatal, usage error.
var porcelainExitCodes = map[string]bool{"0": true, "1": true, "128": true, "129": true}

var exitCodeCell = regexp.MustCompile(`^\|\s*(\d+)\s*\|`)

// docSection returns the lines of doc from heading up to the next heading
// of the same or a higher level.
func docSection(t *testing.T, doc, heading string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != heading {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") || strings.HasPrefix(lines[j], "### ") {
				end = j
				break
			}
		}
		return strings.Join(lines[i:end], "\n")
	}
	t.Fatalf("heading %q not found", heading)
	return ""
}

// cloneFlags is the flags clone mode takes beyond the global ones: those
// the root command registers for itself, less help, version and kit's
// help-group switches.
func cloneFlags() map[string]*pflag.Flag {
	flags := map[string]*pflag.Flag{}
	RootCmd.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "version" || strings.HasPrefix(f.Name, "help") {
			return
		}
		flags[f.Name] = f
	})
	return flags
}

func TestCloneReferenceMatchesCLI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "09-reference.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	section := docSection(t, string(raw), cloneDocHeading)

	actual := cloneFlags()
	documented := map[string]bool{}
	for _, row := range parseGlobalFlagTable(t, section, "**Flags:**") {
		documented[row.long] = true
		f, ok := actual[row.long]
		if !ok {
			t.Errorf("documents --%s, which clone mode does not take", row.long)
			continue
		}
		if row.short != f.Shorthand {
			t.Errorf("documents --%s shorthand as %s, git-hop binds %s", row.long, dashed(row.short), dashed(f.Shorthand))
		}
	}
	var missing []string
	for name := range actual {
		if !documented[name] {
			missing = append(missing, "--"+name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("clone flags missing from the reference: %s", strings.Join(missing, ", "))
	}

	exitTable := false
	for _, l := range strings.Split(section, "\n") {
		if strings.TrimSpace(l) == "**Exit Codes:**" {
			exitTable = true
			continue
		}
		if !exitTable {
			continue
		}
		if m := exitCodeCell.FindStringSubmatch(strings.TrimSpace(l)); m != nil && !porcelainExitCodes[m[1]] {
			t.Errorf("documents exit code %s; git-hop exits 0, 1, 128 or 129", m[1])
		}
	}
	if !exitTable {
		t.Error("clone section has no exit codes table")
	}
}
