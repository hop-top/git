package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestStringLiterals_AreASCII keeps git porcelain's plain-ASCII output
// rule: no check marks, arrows, dashes or box drawing in any string the
// CLI can print. It scans string literals rather than comments, so prose
// in comments stays free. Literals are checked after unquoting, so an
// escape such as "\u2713" counts the same as the glyph it spells.
func TestStringLiterals_AreASCII(t *testing.T) {
	var offenders []string
	forEachSourceFile(t, func(path, rel string) {
		offenders = append(offenders, nonASCIILiterals(t, path, rel)...)
	})

	for _, o := range offenders {
		t.Errorf("non-ASCII string literal: %s", o)
	}
}

// forEachSourceFile calls fn for every non-test Go file the CLI is built
// from, with its path relative to the module root.
func forEachSourceFile(t *testing.T, fn func(path, rel string)) {
	t.Helper()
	root := ".."
	for _, dir := range []string{"cmd", "internal", "main.go"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			fn(path, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
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
		value := lit.Value
		if v, err := strconv.Unquote(lit.Value); err == nil {
			value = v
		}
		for _, r := range value {
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
