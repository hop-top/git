package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
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

// rawStderrAllowed lists the functions outside internal/output that may
// write to os.Stderr directly, keyed "<file>:<func>". Everything else
// goes through output.Warn/Error/Hint/Note/Fatal*, so -q, --porcelain
// and JSON mode apply to it. Keep each entry justified.
var rawStderrAllowed = map[string]string{
	// An interactive prompt and its answer; only reached on a terminal.
	"cmd/add.go:opencodeAgentHint": "interactive OpenCode config prompt",
	// Fails before the CLI, and so the output package, is set up.
	"main.go:main": "xrr cassette install failure, before any command runs",
	// Test-only process setup, never part of a git hop run.
	"internal/testenv/testenv.go:Run": "test harness setup failure",
}

// TestDiagnostics_NotWrittenRawToStderr keeps stderr diagnostics going
// through the output package: a fmt.Fprint* straight to os.Stderr
// ignores -q and prints plain text in JSON mode.
func TestDiagnostics_NotWrittenRawToStderr(t *testing.T) {
	seen := map[string]bool{}
	forEachSourceFile(t, func(path, rel string) {
		if strings.HasPrefix(rel, "internal/output/") {
			return
		}
		for _, w := range rawStderrWrites(t, path, rel) {
			if _, ok := rawStderrAllowed[w.key]; ok {
				seen[w.key] = true
				continue
			}
			t.Errorf("raw write to os.Stderr (use the output package): %s", w.where)
		}
	})
	for key := range rawStderrAllowed {
		if !seen[key] {
			t.Errorf("rawStderrAllowed lists %s, which no longer writes to os.Stderr", key)
		}
	}
}

type rawWrite struct{ key, where string }

// rawStderrWrites finds fmt.Fprint* calls whose writer is os.Stderr,
// keyed by the enclosing top-level function.
func rawStderrWrites(t *testing.T, path, rel string) []rawWrite {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var found []rawWrite
	for _, decl := range file.Decls {
		name := "<package>"
		if fn, ok := decl.(*ast.FuncDecl); ok {
			name = fn.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 || !isOSStderr(call.Args[0]) {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "fmt" || !strings.HasPrefix(sel.Sel.Name, "Fprint") {
				return true
			}
			pos := fset.Position(call.Pos())
			found = append(found, rawWrite{
				key:   rel + ":" + name,
				where: fmt.Sprintf("%s:%d in %s", rel, pos.Line, name),
			})
			return true
		})
	}
	return found
}

func isOSStderr(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os" && sel.Sel.Name == "Stderr"
}
