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

// writerDiagnosticsAllowed lists the functions outside internal/output
// that may print a diagnostic through an io.Writer they were handed,
// keyed "<file>:<func>". Keep each entry justified.
var writerDiagnosticsAllowed = map[string]string{
	// Runs before the output mode is set up, so it reads the output
	// flags from the raw arguments and renders JSON itself.
	"internal/cli/root.go:reportUsageError": "usage error, before the output package is set up",
}

// TestDiagnostics_NotWrittenThroughWriters closes the gap the os.Stderr
// scan leaves: a diagnostic printed with fmt.Fprint* to a writer passed
// down (an opts.Stderr, a cmd.ErrOrStderr()) is as blind to -q and JSON
// mode as a raw os.Stderr write, but names no os.Stderr the other scan
// could see. Any fmt.Fprint* whose message opens with a warning:,
// error:, hint:, note: or fatal: prefix must go through the output
// package instead, whatever its writer.
func TestDiagnostics_NotWrittenThroughWriters(t *testing.T) {
	seen := map[string]bool{}
	forEachSourceFile(t, func(path, rel string) {
		if strings.HasPrefix(rel, "internal/output/") {
			return
		}
		for _, w := range writerDiagnostics(t, path, rel) {
			if _, ok := writerDiagnosticsAllowed[w.key]; ok {
				seen[w.key] = true
				continue
			}
			t.Errorf("diagnostic written through an io.Writer (use output.Warn/Error/Hint): %s", w.where)
		}
	})
	for key := range writerDiagnosticsAllowed {
		if !seen[key] {
			t.Errorf("writerDiagnosticsAllowed lists %s, which no longer writes a diagnostic", key)
		}
	}
}

// writerDiagnostics finds fmt.Fprint* calls to any writer other than
// os.Stdout (TestDiagnostics_NotPrintedToStdout's) whose message literal
// opens with a diagnostic prefix, keyed by the enclosing function.
func writerDiagnostics(t *testing.T, path, rel string) []rawWrite {
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
			if !ok || len(call.Args) < 2 || isOSStdout(call.Args[0]) {
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
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			msg, err := strconv.Unquote(lit.Value)
			if err != nil || !diagnosticPrefix.MatchString(msg) {
				return true
			}
			pos := fset.Position(call.Pos())
			found = append(found, rawWrite{
				key:   rel + ":" + name,
				where: fmt.Sprintf("%s:%d in %s: %s", rel, pos.Line, name, lit.Value),
			})
			return true
		})
	}
	return found
}
