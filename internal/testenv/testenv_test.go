package testenv_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"hop.top/git/internal/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}

// isUnder reports whether path is root or lies beneath it.
func isUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func TestRun_PointsUserLocationsAtThrowawayRoot(t *testing.T) {
	root := testenv.Root()
	if root == "" {
		t.Fatal("Root() is empty; Run did not isolate this binary")
	}
	realHome := testenv.RealHome()
	if realHome != "" && isUnder(root, realHome) {
		t.Fatalf("throwaway root %s lies under the real home %s", root, realHome)
	}

	for _, key := range []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "GIT_CONFIG_GLOBAL",
	} {
		val := os.Getenv(key)
		if val == "" {
			t.Errorf("%s is unset; want a path under %s", key, root)
			continue
		}
		if !isUnder(val, root) {
			t.Errorf("%s=%s escapes the throwaway root %s", key, val, root)
		}
	}

	// Overrides that would bypass the XDG homes above must not leak in.
	for _, key := range []string{"GIT_HOP_DATA_HOME", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		if val, ok := os.LookupEnv(key); ok {
			t.Errorf("%s=%q survived isolation", key, val)
		}
	}
	if os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" {
		t.Error("GIT_CONFIG_NOSYSTEM is not set; the system git config still applies")
	}
}

// TestRun_GitSeesOnlyTheThrowawayConfig proves git itself honours the
// isolation: `git config --global` writes land in the throwaway file, and
// the test identity is what commits would use.
func TestRun_GitSeesOnlyTheThrowawayConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	out, err := exec.Command("git", "config", "--global", "--show-origin", "user.email").Output()
	if err != nil {
		t.Fatalf("git config --global user.email: %v", err)
	}
	if !strings.Contains(string(out), os.Getenv("GIT_CONFIG_GLOBAL")) {
		t.Fatalf("global user.email came from %q, want the throwaway %s", out, os.Getenv("GIT_CONFIG_GLOBAL"))
	}
}

// TestRun_GoToolchainKeepsItsCaches guards the pinning done before HOME
// moves: without it, tests that build binaries would start from an empty
// module and build cache inside the throwaway root.
func TestRun_GoToolchainKeepsItsCaches(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	root := testenv.Root()
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		val := os.Getenv(key)
		if val == "" {
			t.Errorf("%s not pinned", key)
			continue
		}
		if isUnder(val, root) {
			t.Errorf("%s=%s was moved into the throwaway root", key, val)
		}
	}
}

// TestEveryTestPackageIsolates is the regression guard. Each directory
// with _test.go files builds its own test binary, and only a TestMain
// routed through testenv keeps that binary off the real user dirs, so
// every one of them must have it.
func TestEveryTestPackageIsolates(t *testing.T) {
	modRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modRoot, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", modRoot, err)
	}

	testDirs := map[string]bool{}
	isolated := map[string]bool{}
	fset := token.NewFileSet()

	err = filepath.WalkDir(modRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != modRoot && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			// A nested module builds its own test binaries; not ours to police.
			if path != modRoot {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		testDirs[dir] = true

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if testMainUsesTestenv(file) {
			isolated[dir] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var missing []string
	for dir := range testDirs {
		if !isolated[dir] {
			rel, _ := filepath.Rel(modRoot, dir)
			missing = append(missing, rel)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("test packages without a TestMain routed through testenv.Run or testenv.Wrap "+
			"(their tests can write to the real config/data/state/cache homes and global git config):\n  %s",
			strings.Join(missing, "\n  "))
	}
}

// testMainUsesTestenv reports whether file declares TestMain and that
// TestMain calls testenv.Run or testenv.Wrap.
func testMainUsesTestenv(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "testenv" && (sel.Sel.Name == "Run" || sel.Sel.Name == "Wrap") {
				found = true
				return false
			}
			return true
		})
		return found
	}
	return false
}
