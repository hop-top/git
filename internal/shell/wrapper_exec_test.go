package shell_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/hooks"
	"hop.top/git/internal/shell"
)

// The generated wrapper is shell source, so string assertions on it prove
// only that text exists -- not that the shell does the right thing with
// it. These tests source the real block in the real interpreter with a
// stub `git` on PATH and assert the shell's resulting cwd and status.
//
// The three cases that matter:
//
//	exit 0  -> cd into the hub's `current` worktree
//	exit 93 -> do NOT cd, and report 0 (the hook already navigated)
//	exit 1  -> do NOT cd, and propagate 1

// wrapperFixture is a hub layout plus a stub git, laid out on the real
// filesystem so a real shell can walk it.
type wrapperFixture struct {
	dir       string // where the shell starts (the "hub")
	worktree  string // what `current` points at -- the expected cd target
	binDir    string // holds the stub git
	stubGitAt string
}

// newWrapperFixture builds a hub whose `current` symlink resolves to a
// worktree dir, and a stub `git` that exits with the requested status for
// `hop` while still answering `rev-parse --show-toplevel` (the wrapper
// calls the real-looking git for both).
func newWrapperFixture(t *testing.T, hopExit int) wrapperFixture {
	t.Helper()

	root := t.TempDir()
	hub := filepath.Join(root, "hub")
	worktree := filepath.Join(root, "worktree")
	binDir := filepath.Join(root, "bin")

	for _, d := range []string{hub, worktree, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// The wrapper probes "$hub_root/../current" first, then
	// "$hub_root/current". Provide the latter.
	if err := os.Symlink(worktree, filepath.Join(hub, "current")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Stub git: `git hop ...` exits with hopExit; `git rev-parse
	// --show-toplevel` prints the hub. Anything else is a no-op success.
	stub := "#!/bin/sh\n" +
		"if [ \"$1\" = \"hop\" ]; then exit " + itoa(hopExit) + "; fi\n" +
		"if [ \"$1\" = \"rev-parse\" ]; then echo \"" + hub + "\"; exit 0; fi\n" +
		"exit 0\n"
	stubGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(stubGit, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub git: %v", err)
	}

	return wrapperFixture{dir: hub, worktree: worktree, binDir: binDir, stubGitAt: stubGit}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for n := i; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}
	return digits
}

// runWrapper sources the generated wrapper in `shellBin`, invokes
// `git-hop` with the given argument, and reports the shell's final cwd and
// the wrapper's exit status.
func runWrapper(t *testing.T, shellBin, shellType string, fx wrapperFixture, arg string) (cwd string, status int) {
	t.Helper()

	src := shell.GenerateWrapperFunction(shellType)
	scriptPath := filepath.Join(t.TempDir(), "probe."+shellType)

	invocation := "git-hop " + arg
	if arg == "" {
		invocation = "git-hop"
	}

	var script string
	if shellType == "fish" {
		// fish: capture the wrapper's status, print cwd, exit with it.
		script = src + "\n" + invocation + "\nset -l rc $status\npwd\nexit $rc\n"
	} else {
		script = src + "\n" + invocation + "\nrc=$?\npwd\nexit $rc\n"
	}

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	// Start-up files must be suppressed: a real user's zshenv/bashrc
	// rewrites PATH, which puts the machine's actual git ahead of the
	// stub and makes the probe exercise the wrong binary entirely.
	var args []string
	switch shellType {
	case "zsh":
		args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
	case "bash":
		args = []string{"--noprofile", "--norc", scriptPath}
	case "fish":
		args = []string{"--no-config", scriptPath}
	default:
		args = []string{scriptPath}
	}

	cmd := exec.Command(shellBin, args...)
	cmd.Dir = fx.dir
	// Stub git must win over the real one.
	cmd.Env = append(os.Environ(), "PATH="+fx.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			status = ee.ExitCode()
		} else {
			t.Fatalf("run %s: %v", shellBin, err)
		}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		t.Fatalf("%s produced no output", shellBin)
	}
	cwd = strings.TrimSpace(lines[len(lines)-1])

	// Resolve symlinks on both sides: macOS /var -> /private/var makes a
	// raw string compare meaningless.
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	return cwd, status
}

// lookShell finds an interpreter, skipping the test when it is absent
// rather than failing -- not every machine has fish.
func lookShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available on this machine: %v", name, err)
	}
	return path
}

func resolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

// TestWrapperExec_ExitCodeBehaviour executes the generated wrapper in every
// interpreter present and pins the cd/status contract for 0, 93 and 1.
func TestWrapperExec_ExitCodeBehaviour(t *testing.T) {
	shells := []struct {
		bin       string
		shellType string
	}{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	}

	cases := []struct {
		name       string
		hopExit    int
		wantStatus int
		wantCd     bool
	}{
		{
			name:       "exit 0 cds into current worktree",
			hopExit:    0,
			wantStatus: 0,
			wantCd:     true,
		},
		{
			// The whole point of this change: the hook already moved the
			// user, so the wrapper must not cd, and must not surface the
			// directive as a failure.
			name:       "navigation-handled directive does not cd and reports success",
			hopExit:    hooks.ExitNavigationHandled,
			wantStatus: 0,
			wantCd:     false,
		},
		{
			name:       "genuine failure does not cd and propagates status",
			hopExit:    1,
			wantStatus: 1,
			wantCd:     false,
		},
	}

	for _, sh := range shells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					fx := newWrapperFixture(t, tc.hopExit)
					cwd, status := runWrapper(t, bin, sh.shellType, fx, "somebranch")

					if status != tc.wantStatus {
						t.Errorf("%s wrapper: exit status = %d, want %d (git hop exited %d)",
							sh.shellType, status, tc.wantStatus, tc.hopExit)
					}

					wantDir := resolve(t, fx.dir)
					if tc.wantCd {
						wantDir = resolve(t, fx.worktree)
					}

					if cwd != wantDir {
						t.Errorf("%s wrapper: cwd = %q, want %q (git hop exited %d, wantCd=%v)",
							sh.shellType, cwd, wantDir, tc.hopExit, tc.wantCd)
					}
				})
			}
		})
	}
}

// TestWrapperExec_EligibilityIsIdenticalAcrossShells pins the should_cd
// decision per shell, executed rather than string-matched.
//
// This exists because the fish block used bash's '[!-]*' bracket class,
// which fish's switch cannot evaluate -- it matches with wildcards only.
// Every branch name therefore fell through with should_cd unset and fish
// users never got the auto-cd at all. Text assertions on the generated
// source cannot see that; only running fish can.
func TestWrapperExec_EligibilityIsIdenticalAcrossShells(t *testing.T) {
	args := []struct {
		arg    string
		wantCd bool
	}{
		{"somebranch", true},
		{"feature/nested-name", true},
		{"add", true},
		{"init", true},
		{"clone", true},
		{"list", false},
		{"status", false},
		{"doctor", false},
		{"prune", false},
		{"env", false},
		{"--help", false},
		{"-h", false},
		{"--version", false},
		{"-v", false},
	}

	for _, sh := range []struct{ bin, shellType string }{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	} {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			for _, a := range args {
				t.Run(a.arg, func(t *testing.T) {
					fx := newWrapperFixture(t, 0)
					cwd, status := runWrapper(t, bin, sh.shellType, fx, a.arg)

					if status != 0 {
						t.Errorf("%s wrapper: exit status = %d, want 0", sh.shellType, status)
					}

					wantDir := resolve(t, fx.dir)
					if a.wantCd {
						wantDir = resolve(t, fx.worktree)
					}
					if cwd != wantDir {
						t.Errorf("%s wrapper: arg %q gave cwd %q, want %q (wantCd=%v)",
							sh.shellType, a.arg, cwd, wantDir, a.wantCd)
					}
				})
			}
		})
	}
}
