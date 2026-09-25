package git

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fetchLiteralAllowed counts the "fetch" string literals that are not a
// git fetch, per file (slash path from the module root).
var fetchLiteralAllowed = map[string]int{
	"internal/git/fetch.go":                 1, // FetchArgs itself
	"cmd/add.go":                            2, // the --[no-]fetch flag name
	"internal/services/package_managers.go": 1, // `cargo fetch`
	"internal/git/gittrace/gittrace.go":     1, // reads a traced fetch
}

// Every git fetch git-hop runs must be built by FetchArgs, which keeps
// git's auto-maintenance off it (see FetchArgs for the race). A bare
// "fetch" literal anywhere else in non-test code is a fetch that bypasses
// it; route it through FetchArgs, or allow it above if it is not git's.
func TestEveryFetchGoesThroughFetchArgs(t *testing.T) {
	modRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modRoot, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", modRoot, err)
	}

	found := map[string][]string{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(modRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != modRoot && (name == "testdata" || name == "vendor" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if path != modRoot {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(modRoot, path)
		rel = filepath.ToSlash(rel)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && s == "fetch" {
				found[rel] = append(found[rel], fset.Position(lit.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for rel, positions := range found {
		if len(positions) > fetchLiteralAllowed[rel] {
			t.Errorf("%s has %d \"fetch\" literal(s), %d allowed; build git fetches with FetchArgs:\n  %s",
				rel, len(positions), fetchLiteralAllowed[rel], strings.Join(positions, "\n  "))
		}
	}
}

func TestFetchArgs(t *testing.T) {
	got := strings.Join(FetchArgs("--quiet", "origin"), " ")
	if want := "-c maintenance.auto=false fetch --quiet origin"; got != want {
		t.Errorf("FetchArgs = %q, want %q", got, want)
	}
}
