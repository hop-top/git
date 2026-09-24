package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"testing"
)

// diagnosticPrefix matches a message that announces itself as a
// warning, error, hint, note or fatal: "Warning: ...", "\nwarning: ...",
// "Warnings:", "Rollback error:", "\nNote: ..." and the like. A note is
// advice, which git prints as hint: on stderr.
var diagnosticPrefix = regexp.MustCompile(`(?i)^\s*(\w+ )?(warnings?|errors?|hints?|notes?|fatal)\s*:`)

// TestDiagnostics_NotPrintedToStdout keeps git's stream split: results on
// stdout, warnings/errors/hints on stderr via output.Warn / output.Error /
// output.Hint (lowercase prefix, quiet- and JSON-aware). A fmt.Print* call, or an
// Fprint* to os.Stdout, whose message opens with such a prefix is the
// defect this guards against.
func TestDiagnostics_NotPrintedToStdout(t *testing.T) {
	forEachSourceFile(t, func(path, rel string) {
		for _, o := range stdoutDiagnostics(t, path, rel) {
			t.Errorf("diagnostic printed to stdout (use output.Warn/Error/Hint): %s", o)
		}
	})
}

func stdoutDiagnostics(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "fmt" {
			return true
		}

		args := call.Args
		switch sel.Sel.Name {
		case "Print", "Printf", "Println":
		case "Fprint", "Fprintf", "Fprintln":
			if len(args) == 0 || !isOSStdout(args[0]) {
				return true
			}
			args = args[1:]
		default:
			return true
		}
		if len(args) == 0 {
			return true
		}
		lit, ok := args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		msg, err := strconv.Unquote(lit.Value)
		if err == nil && diagnosticPrefix.MatchString(msg) {
			pos := fset.Position(call.Pos())
			found = append(found, fmt.Sprintf("%s:%d: %s", rel, pos.Line, lit.Value))
		}
		return true
	})
	return found
}

func isOSStdout(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os" && sel.Sel.Name == "Stdout"
}
