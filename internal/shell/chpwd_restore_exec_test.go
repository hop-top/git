package shell_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hop.top/git/internal/hooks"
)

// The handled-navigation directive on the chdir path.
//
// A post-worktree-switch hook that navigates -- a tmux window-per-worktree
// integration selecting the destination's window -- reports 93 so the shell
// wrapper does not cd on top of it. The chdir path needs the same signal for
// the opposite reason: nothing here is WAITING to cd, the user already cd'd,
// and that cd is precisely what contaminated the originating pane. So the
// directive means "undo the user's cd", not "skip a cd".
//
// Like the rest of this package's exec tests, the assertions run the real
// interpreters. What matters is where the shell ENDS UP, which no string
// test on the generated source can observe.

// runChdirScriptForPwd sources the handler, performs a sequence of cds, and
// prints $PWD as the last line -- the instrument for the restore behaviour.
//
// The fork log answers "did the handler call the binary"; it cannot answer
// "where did the handler leave the shell". Only the shell's own final $PWD
// can, so this variant reports it alongside the log.
func runChdirScriptForPwd(t *testing.T, shellBin, shellType, handlerSrc string, fx chdirFixture, cds []string, startDir string) (pwd string, forks []string) {
	t.Helper()

	var b strings.Builder
	b.WriteString(handlerSrc)
	b.WriteString("\n")

	for _, d := range cds {
		b.WriteString("cd '" + d + "'\n")
		if shellType == "bash" {
			b.WriteString("eval \"$PROMPT_COMMAND\"\n")
		}
	}
	b.WriteString("printf '%s\\n' \"$PWD\"\n")

	scriptPath := filepath.Join(t.TempDir(), "probe."+shellType)
	if err := os.WriteFile(scriptPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	var args []string
	switch shellType {
	case "zsh":
		args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
	case "bash":
		args = []string{"--noprofile", "--norc", scriptPath}
	case "fish":
		args = []string{"--no-config", scriptPath}
	}

	cmd := exec.Command(shellBin, args...)
	cmd.Dir = startDir
	cmd.Env = append(os.Environ(), "PATH="+fx.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("run %s: %v (output: %s)", shellBin, err, out)
		}
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	pwd = strings.TrimSpace(lines[len(lines)-1])
	if resolved, err := filepath.EvalSymlinks(pwd); err == nil {
		pwd = resolved
	}

	return pwd, readForkLog(t, fx.forkLog)
}

// stubGitExiting replaces the fixture's stub git with one that still records
// its argv but exits with the given status, standing in for a binary whose
// post-worktree-switch hook returned the handled-navigation directive.
func stubGitExiting(t *testing.T, fx chdirFixture, code int) {
	t.Helper()
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + fx.forkLog + "\"\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(fx.binDir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub git: %v", err)
	}
}

// TestChdirExec_NavigationHandledRestoresOrigin is the regression test for
// the contamination this fix exists to stop.
//
// A hook that navigates (a tmux window-per-worktree integration selecting
// the destination's window) leaves the user looking at the destination in a
// DIFFERENT window. The shell that ran the plain cd is still sitting in the
// originating pane -- and that pane's $PWD is now the destination too, so
// the origin worktree's window and the destination's window both point into
// the destination. The handler has to put the originating shell back where
// it came from.
func TestChdirExec_NavigationHandledRestoresOrigin(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)
			stubGitExiting(t, fx, hooks.ExitNavigationHandled)

			// A -> B. The first arrival has no registered origin, so only
			// the second can be restored; that is the case that matters.
			pwd, forks := runChdirScriptForPwd(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeB}, fx.root)

			if len(forks) != 2 {
				t.Fatalf("%s: expected 2 notifies, got %d: %v", sh.shellType, len(forks), forks)
			}
			if pwd != fx.worktreeA {
				t.Errorf("%s: handler left the shell in %q, want it restored to the origin %q",
					sh.shellType, pwd, fx.worktreeA)
			}
		})
	}
}

// TestChdirExec_NoDirectiveLeavesShellAtDestination is the other half of the
// contract: without the directive nothing navigated on the user's behalf, so
// their cd must stand exactly as they typed it.
func TestChdirExec_NoDirectiveLeavesShellAtDestination(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)
			// Stub git exits 0 -- the fixture default, restated for clarity.
			stubGitExiting(t, fx, 0)

			pwd, forks := runChdirScriptForPwd(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeB}, fx.root)

			if len(forks) != 2 {
				t.Fatalf("%s: expected 2 notifies, got %d: %v", sh.shellType, len(forks), forks)
			}
			if pwd != fx.worktreeB {
				t.Errorf("%s: handler moved the shell to %q; with no directive the user's cd to %q must stand",
					sh.shellType, pwd, fx.worktreeB)
			}
		})
	}
}

// TestChdirExec_RestoreDoesNotReenter pins that the restoring cd happens
// inside the re-entrancy guard.
//
// The restore is itself a cd between two registered worktrees, which is
// exactly the transition this handler fires on. Performed outside the guard
// it re-enters, notifies for the origin, gets the directive again, and cds
// back -- a ping-pong that never settles. One notify per user cd is the
// whole assertion.
func TestChdirExec_RestoreDoesNotReenter(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)
			stubGitExiting(t, fx, hooks.ExitNavigationHandled)

			_, forks := runChdirScriptForPwd(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeB}, fx.root)

			if len(forks) != 2 {
				t.Errorf("%s: A->B under the directive produced %d notify(s), want exactly 2: %v",
					sh.shellType, len(forks), forks)
			}
		})
	}
}

// TestChdirExec_VanishedOriginIsNotRestored covers the origin disappearing
// between the cd and the directive -- a worktree removed by another shell,
// say. cd'ing into a path that no longer exists would strand the user in a
// deleted directory, which is worse than the contamination being fixed, so
// the restore is conditional on the origin still being there.
func TestChdirExec_VanishedOriginIsNotRestored(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			// The stub removes the origin before reporting the directive,
			// standing in for a hook whose side effects took it away.
			stub := "#!/bin/sh\n" +
				"printf '%s\\n' \"$*\" >> \"" + fx.forkLog + "\"\n" +
				"rm -rf \"" + fx.worktreeA + "\"\n" +
				"exit " + strconv.Itoa(hooks.ExitNavigationHandled) + "\n"
			if err := os.WriteFile(filepath.Join(fx.binDir, "git"), []byte(stub), 0o755); err != nil {
				t.Fatalf("write stub git: %v", err)
			}

			pwd, _ := runChdirScriptForPwd(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeB}, fx.root)

			if pwd != fx.worktreeB {
				t.Errorf("%s: shell ended in %q; a vanished origin must leave the shell at %q rather than a deleted path",
					sh.shellType, pwd, fx.worktreeB)
			}
		})
	}
}
