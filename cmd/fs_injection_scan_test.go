package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
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

// osOnFsHolderAllowed lists methods on a type holding an afero.Fs that
// deliberately act on the real disk, keyed "<file> <Type>.<Method>".
var osOnFsHolderAllowed = map[string]string{
	"internal/services/trash.go Trash.Move": "os.Rename only when the held fs is *afero.OsFs: a " +
		"same-device rename, which afero's copy fallback cannot express; other fs values take the copy path",
	"internal/services/trash.go Trash.Restore": "same OsFs-guarded os.Rename as Trash.Move",
	"internal/hop/conversion_index.go Converter.gitDiff": "git writes the diff to a real scratch " +
		"file (--output); the converter reads it back and removes it from the disk git wrote to",
	"internal/hop/conversion_index.go Converter.applyCached": "git apply reads the patch from a real " +
		"scratch file, so it is written to and removed from the disk git reads",
}

// TestFsParamFuncs_DoNotCallOSFilesystem keeps a function that takes an
// afero.Fs on that filesystem: an os call beside it acts on the real disk
// whatever fs the caller injected (an in-memory one in tests, a
// read-only or copy-on-write view in a dry run). Closures inside such a
// function count. Symlinks go through afero.Linker / afero.LinkReader /
// afero.Lstater.
func TestFsParamFuncs_DoNotCallOSFilesystem(t *testing.T) {
	forEachSourceFile(t, func(path, rel string) {
		file, fset := parseSource(t, path)
		for _, fn := range funcDecls(file) {
			if !takesAferoFs(fn.Type) {
				continue
			}
			for _, o := range osFilesystemCallsIn(fset, rel, fn) {
				t.Errorf("os filesystem call in a function given an afero.Fs (use the fs): %s", o)
			}
		}
	})
}

// TestFsHolderMethods_DoNotCallOSFilesystem is the same rule for methods
// whose receiver type holds an afero.Fs field: the fs arrived through the
// constructor rather than a parameter, and an os call beside it bypasses
// it just the same. Deliberate real-disk access is listed, with its
// reason, in osOnFsHolderAllowed.
func TestFsHolderMethods_DoNotCallOSFilesystem(t *testing.T) {
	type source struct {
		rel  string
		file *ast.File
		fset *token.FileSet
	}
	var sources []source
	holders := map[string]bool{} // "<package dir> <Type>"
	forEachSourceFile(t, func(path, rel string) {
		file, fset := parseSource(t, path)
		sources = append(sources, source{rel, file, fset})
		for _, name := range fsHolderTypes(file) {
			holders[filepath.Dir(rel)+" "+name] = true
		}
	})

	used := map[string]bool{}
	for _, src := range sources {
		for _, fn := range funcDecls(src.file) {
			recv := receiverTypeName(fn)
			if recv == "" || !holders[filepath.Dir(src.rel)+" "+recv] {
				continue
			}
			calls := osFilesystemCallsIn(src.fset, src.rel, fn)
			if len(calls) == 0 {
				continue
			}
			key := src.rel + " " + recv + "." + fn.Name.Name
			if _, ok := osOnFsHolderAllowed[key]; ok {
				used[key] = true
				continue
			}
			for _, o := range calls {
				t.Errorf("os filesystem call in a method of %s, which holds an afero.Fs (use the fs): %s", recv, o)
			}
		}
	}
	for key := range osOnFsHolderAllowed {
		if !used[key] {
			t.Errorf("stale osOnFsHolderAllowed entry %q: no os filesystem call left there", key)
		}
	}
}

func parseSource(t *testing.T, path string) (*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file, fset
}

func funcDecls(file *ast.File) []*ast.FuncDecl {
	var fns []*ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			fns = append(fns, fn)
		}
	}
	return fns
}

// osFilesystemCallsIn reports the os filesystem calls in fn's body,
// closures included.
func osFilesystemCallsIn(fset *token.FileSet, rel string, fn *ast.FuncDecl) []string {
	var found []string
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
	return found
}

func isAferoFs(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "afero" && sel.Sel.Name == "Fs"
}

func takesAferoFs(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, p := range ft.Params.List {
		if isAferoFs(p.Type) {
			return true
		}
	}
	return false
}

// fsHolderTypes names the struct types declared in file with an
// afero.Fs field, embedded or named.
func fsHolderTypes(file *ast.File) []string {
	var names []string
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			if isAferoFs(f.Type) {
				names = append(names, ts.Name.Name)
				break
			}
		}
		return true
	})
	return names
}

// receiverTypeName is the base type name of fn's receiver, "" for a
// plain function.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.IndexExpr:
		expr = e.X
	case *ast.IndexListExpr:
		expr = e.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
