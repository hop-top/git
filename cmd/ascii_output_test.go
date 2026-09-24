package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// asciiPendingDirs hold the output package's shared glyph set (status
// icons, card borders, spinner frames, progress bars). Replacing it
// changes how list and status render their tables, so it is tracked
// separately; everything else must already be plain ASCII.
var asciiPendingDirs = []string{
	filepath.Join("internal", "output"),
}

// TestStringLiterals_AreASCII keeps git porcelain's plain-ASCII output
// rule: no check marks, arrows, dashes or box drawing in any string the
// CLI can print. It scans string literals rather than comments, so prose
// in comments stays free.
func TestStringLiterals_AreASCII(t *testing.T) {
	root := ".."
	var offenders []string

	for _, dir := range []string{"cmd", "internal", "main.go"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if d.IsDir() {
				for _, p := range asciiPendingDirs {
					if rel == p {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			offenders = append(offenders, nonASCIILiterals(t, path, rel)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	for _, o := range offenders {
		t.Errorf("non-ASCII string literal: %s", o)
	}
}

func nonASCIILiterals(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
			return true
		}
		for _, r := range lit.Value {
			if r > 0x7f {
				pos := fset.Position(lit.Pos())
				found = append(found, fmt.Sprintf("%s:%d: %s", rel, pos.Line, lit.Value))
				break
			}
		}
		return true
	})
	return found
}
