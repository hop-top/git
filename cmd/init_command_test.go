package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// initHintCommands are the commands init's hints offer, by hint.
var initHintCommands = map[string]func(*pflag.FlagSet) string{
	"proceed":  initProceedCommand,
	"force":    initForceCommand,
	"retry":    initRetryCommand,
	"regular":  initRegularCommand,
	"register": initConvertCommand,
	"restore":  func(*pflag.FlagSet) string { return initRestoreCommand("/bk/proj", true) },
}

// TestInitHintCommands pins the command each init hint offers: the
// flags the user gave that still apply, as given, plus those the hint
// adds, never twice, in one fixed order.
func TestInitHintCommands(t *testing.T) {
	cases := []struct {
		hint string
		args []string
		want string
	}{
		// Dirty-tree refusal, "Then run:": the same conversion once the
		// tree is clean.
		{"proceed", []string{"--no-prompt", "--regular", "--no-hooks"},
			"git hop init --no-prompt --regular --no-hooks"},
		// Dirty-tree refusal, convert anyway: the same conversion, forced.
		{"force", nil, "git hop init --force"},
		{"force", []string{"--no-prompt", "--regular", "--no-hooks"},
			"git hop init --no-prompt --regular --force --no-hooks"},
		{"force", []string{"--hooks", "copy", "--keep-backup=false"},
			"git hop init --force --keep-backup=false --hooks=copy"},
		{"force", []string{"--force=false", "--no-prompt"},
			"git hop init --no-prompt --force"},
		// Detached HEAD / operation in progress: the command as run,
		// dry run included; --force kept, it does not lift the refusal
		// but is the user's consent for the conversion they retry.
		{"retry", nil, "git hop init"},
		{"retry", []string{"--no-prompt", "--no-hooks", "-n"},
			"git hop init --no-prompt --no-hooks --dry-run"},
		{"retry", []string{"--dry-run", "--regular", "--no-prompt", "--force", "--enable-chdir"},
			"git hop init --no-prompt --regular --force --enable-chdir --dry-run"},
		// Linked worktrees: the regular layout, which --regular only
		// selects together with --no-prompt.
		{"regular", nil, "git hop init --no-prompt --regular"},
		{"regular", []string{"--no-prompt", "--no-hooks", "--force"},
			"git hop init --no-prompt --regular --force --no-hooks"},
		{"regular", []string{"--regular", "--no-prompt"}, "git hop init --no-prompt --regular"},
		// Registered as-is: a conversion with the hook and shell choices
		// made for the registration.
		{"register", nil, "git hop init --no-prompt"},
		{"register", []string{"--no-hooks", "--enable-chdir", "-n"},
			"git hop init --no-prompt --no-hooks --enable-chdir"},
		// Restore: none of the conversion flags apply.
		{"restore", []string{"--no-prompt", "--regular", "--no-hooks", "--keep-backup", "-n"},
			"git hop init --restore /bk/proj --force"},
	}
	for _, tc := range cases {
		got := initHintCommands[tc.hint](initTestFlags(t, tc.args...))
		if got != tc.want {
			t.Errorf("%s hint after %v: got %q, want %q", tc.hint, tc.args, got, tc.want)
		}
	}
}

// Every flag any init hint can name is one init really declares.
func TestInitHintCommands_NameDeclaredFlags(t *testing.T) {
	all := []string{"-n", "--no-prompt", "--regular", "--force", "--keep-backup",
		"--no-hooks", "--hooks", "copy", "--hooks-overwrite", "--enable-chdir"}
	for hint, build := range initHintCommands {
		for _, tok := range strings.Fields(build(initTestFlags(t, all...))) {
			name, ok := strings.CutPrefix(tok, "--")
			if !ok {
				continue
			}
			name, _, _ = strings.Cut(name, "=")
			if initCmd.Flags().Lookup(name) == nil {
				t.Errorf("%s hint names --%s, which init does not declare", hint, name)
			}
		}
	}
}

// TestInitHints_BuiltByOneHelper fails on a `git hop init` command
// spelled out in an init hint rather than built by initCommandLine,
// where it would drift from the flags the user gave: string literals in
// cmd/init*.go, and the refusal hints internal/hop hands to init.
func TestInitHints_BuiltByOneHelper(t *testing.T) {
	files, err := filepath.Glob("init*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"conversion_branch.go", "conversion_inprogress.go"} {
		files = append(files, filepath.Join("..", "internal", "hop", f))
	}
	scanned := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		scanned++
		inHop := strings.Contains(path, filepath.Join("internal", "hop"))
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if isFunc && fn.Name.Name == "initCommandLine" {
				continue
			}
			// internal/hop's messages may name init in prose; only the
			// hints it returns suggest running it.
			if inHop && (!isFunc || fn.Name.Name != "Hints") {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					s = lit.Value
				}
				if strings.Contains(s, "git hop init") {
					t.Errorf("%s: hint spells out %q; build it with initCommandLine",
						fset.Position(lit.Pos()), s)
				}
				return true
			})
		}
	}
	if scanned < 10 {
		t.Fatalf("scanned %d files; the glob no longer finds init's sources", scanned)
	}
}
