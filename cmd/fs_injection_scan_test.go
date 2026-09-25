package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// osFilesystemCalls are the os functions that touch the filesystem.
var osFilesystemCalls = map[string]bool{
	"Stat": true, "Lstat": true, "ReadFile": true, "ReadDir": true,
	"Open": true, "OpenFile": true, "Create": true, "WriteFile": true,
	"Mkdir": true, "MkdirAll": true, "MkdirTemp": true, "CreateTemp": true,
	"Remove": true, "RemoveAll": true, "Rename": true,
	"Symlink": true, "Readlink": true, "Link": true,
	"Chmod": true, "Chown": true, "Lchown": true, "Chtimes": true, "Truncate": true,
}

// TestFsParamFuncs_DoNotCallOSFilesystem keeps a function that takes an
// afero.Fs on that filesystem: an os call beside it acts on the real disk
// whatever fs the caller injected (an in-memory one in tests, a
// read-only or copy-on-write view in a dry run). Closures inside such a
// function count. Symlinks go through afero.Linker / afero.LinkReader /
// afero.Lstater.
func TestFsParamFuncs_DoNotCallOSFilesystem(t *testing.T) {
	forEachSourceFile(t, func(path, rel string) {
		for _, o := range osCallsBesideFs(t, path, rel) {
			t.Errorf("os filesystem call in a function given an afero.Fs (use the fs): %s", o)
		}
	})
}

func osCallsBesideFs(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var found []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !takesAferoFs(fn.Type) {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" && osFilesystemCalls[sel.Sel.Name] {
				pos := fset.Position(call.Pos())
				found = append(found, fmt.Sprintf("%s:%d: %s: os.%s", rel, pos.Line, fn.Name.Name, sel.Sel.Name))
			}
			return true
		})
	}
	return found
}

func takesAferoFs(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, p := range ft.Params.List {
		sel, ok := p.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "afero" && sel.Sel.Name == "Fs" {
			return true
		}
	}
	return false
}
