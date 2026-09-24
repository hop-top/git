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

// globalFlagTables names every doc page carrying a global flags table and
// the heading that opens it. The table must describe the flags the root
// command actually registers, so a renamed, dropped or re-lettered flag
// fails here instead of shipping as stale docs.
var globalFlagTables = []struct {
	file    string
	heading string
}{
	{"04-commands.mdx", "## Global Flags"},
	{"09-reference.mdx", "### Global Flags"},
}

// documentedFlag is one row of a global flags table: its long name and,
// when the row lists one, its single-letter shorthand.
type documentedFlag struct {
	long, short string
}

// flagCell matches the first cell of a table row, e.g. "| `--quiet, -q` |".
var flagCell = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|")

// parseGlobalFlagTable reads the table that follows heading in doc.
func parseGlobalFlagTable(t *testing.T, doc, heading string) []documentedFlag {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("heading %q not found", heading)
	}

	var rows []documentedFlag
	inTable := false
	for _, l := range lines[start+1:] {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		m := flagCell.FindStringSubmatch(l)
		if m == nil {
			continue // header or separator row
		}
		var row documentedFlag
		for _, part := range strings.Split(m[1], ",") {
			name := strings.Fields(strings.TrimSpace(part))[0]
			switch {
			case strings.HasPrefix(name, "--"):
				row.long = strings.TrimPrefix(name, "--")
			case strings.HasPrefix(name, "-"):
				row.short = strings.TrimPrefix(name, "-")
			}
		}
		if row.long == "" {
			t.Errorf("row %q names no long flag", l)
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatalf("no flag rows under %q", heading)
	}
	return rows
}

// rootGlobalFlags is the flag set every git-hop invocation accepts: the
// root's visible persistent flags, plus --help and the top-level --version.
func rootGlobalFlags() map[string]*pflag.Flag {
	RootCmd.InitDefaultHelpFlag()
	RootCmd.InitDefaultVersionFlag()

	flags := map[string]*pflag.Flag{}
	RootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			flags[f.Name] = f
		}
	})
	for _, name := range []string{"help", "version"} {
		if f := RootCmd.Flags().Lookup(name); f != nil {
			flags[name] = f
		}
	}
	return flags
}

func TestGlobalFlagsDocsMatchRootFlags(t *testing.T) {
	actual := rootGlobalFlags()

	for _, tbl := range globalFlagTables {
		t.Run(tbl.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "docs", tbl.file))
			if err != nil {
				t.Fatal(err)
			}
			rows := parseGlobalFlagTable(t, string(raw), tbl.heading)

			documented := map[string]documentedFlag{}
			for _, row := range rows {
				if _, dup := documented[row.long]; dup {
					t.Errorf("--%s listed twice", row.long)
				}
				documented[row.long] = row

				f, ok := actual[row.long]
				if !ok {
					t.Errorf("documents --%s, which git-hop does not register", row.long)
					continue
				}
				switch {
				case f.Shorthand != "":
					if row.short != f.Shorthand {
						t.Errorf("documents --%s shorthand as %s, git-hop binds -%s",
							row.long, dashed(row.short), f.Shorthand)
					}
				case row.short != "":
					// A shorthand the root does not bind yet is tolerated so
					// docs can land alongside the flag change; one that
					// already belongs to another flag is always wrong.
					for _, fs := range []*pflag.FlagSet{RootCmd.PersistentFlags(), RootCmd.Flags()} {
						if owner := fs.ShorthandLookup(row.short); owner != nil {
							t.Errorf("documents -%s for --%s, but -%s is --%s",
								row.short, row.long, row.short, owner.Name)
						}
					}
				}
			}

			var missing []string
			for name := range actual {
				if _, ok := documented[name]; !ok {
					missing = append(missing, "--"+name)
				}
			}
			sort.Strings(missing)
			if len(missing) > 0 {
				t.Errorf("global flags missing from the table: %s", strings.Join(missing, ", "))
			}
		})
	}
}

func dashed(short string) string {
	if short == "" {
		return "(none)"
	}
	return "-" + short
}
