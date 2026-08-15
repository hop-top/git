package hooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// writeExecutableHook drops an executable script at the hook path for
// hookName under worktreePath, using the real OS filesystem. These tests
// must actually fork the script, so afero.NewMemMapFs is not usable here --
// exec.Command reads through the OS, not through afero.
func writeExecutableHook(t *testing.T, worktreePath, hookName, script string) {
	t.Helper()
	hookPath := filepath.Join(worktreePath, ".git-hop", "hooks", hookName)
	if err := os.MkdirAll(filepath.Dir(hookPath), 0755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
		t.Fatalf("write hook %s: %v", hookName, err)
	}
}

// exitingHook builds a hook script that prints a line to stdout and then
// exits with the given status.
func exitingHook(stdout string, code int) string {
	return "#!/bin/sh\necho '" + stdout + "'\nexit " + strconv.Itoa(code) + "\n"
}

// runBothVariants exercises a case through ExecuteHook and
// ExecuteHookWithDetector so the two entry points can never diverge on
// exit-code handling.
func runBothVariants(t *testing.T, hookName, worktreePath string) map[string]struct {
	res RunResult
	err error
} {
	t.Helper()
	runner := NewRunner(afero.NewOsFs())

	plainRes, plainErr := runner.ExecuteHook(hookName, worktreePath, "github.com/test/repo", "feature-x")
	detRes, detErr := runner.ExecuteHookWithDetector(
		hookName, worktreePath, "github.com/test/repo", "feature-x",
		map[string]string{"GIT_HOP_BRANCH_TYPE": "feature"},
	)

	return map[string]struct {
		res RunResult
		err error
	}{
		"ExecuteHook":             {plainRes, plainErr},
		"ExecuteHookWithDetector": {detRes, detErr},
	}
}

// TestHookExitCodes_ThreeClasses pins the three exit-status classes for
// post-worktree-switch: success, the reserved handled-navigation code, and
// a generic failure.
func TestHookExitCodes_ThreeClasses(t *testing.T) {
	tests := []struct {
		name        string
		exitCode    int
		wantHandled bool
		wantErr     bool
	}{
		{"zero is plain success", 0, false, false},
		{"reserved code is handled navigation", ExitNavigationHandled, true, false},
		{"generic non-zero is a failure", 1, false, true},
		{"other non-zero is a failure", 42, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worktreePath := t.TempDir()
			writeExecutableHook(t, worktreePath, "post-worktree-switch", exitingHook("hi", tt.exitCode))

			for variant, got := range runBothVariants(t, "post-worktree-switch", worktreePath) {
				if got.res.NavigationHandled != tt.wantHandled {
					t.Errorf("%s: NavigationHandled = %v, want %v (err=%v)",
						variant, got.res.NavigationHandled, tt.wantHandled, got.err)
				}
				if (got.err != nil) != tt.wantErr {
					t.Errorf("%s: err = %v, wantErr %v", variant, got.err, tt.wantErr)
				}
			}
		})
	}
}

// TestHookExitCodes_ReservedCodeIsNotAnError guards the core claim: a hook
// that handled navigation SUCCEEDED. If this ever regressed into the error
// return, callers would abort a switch that actually completed.
func TestHookExitCodes_ReservedCodeIsNotAnError(t *testing.T) {
	worktreePath := t.TempDir()
	writeExecutableHook(t, worktreePath, "post-worktree-switch", exitingHook("navigated", ExitNavigationHandled))

	for variant, got := range runBothVariants(t, "post-worktree-switch", worktreePath) {
		if got.err != nil {
			t.Errorf("%s: reserved exit code surfaced as an error: %v", variant, got.err)
		}
		if !got.res.NavigationHandled {
			t.Errorf("%s: reserved exit code did not set NavigationHandled", variant)
		}
	}
}

// TestHookExitCodes_ReservedCodeIsInertForOtherHooks proves the signal is
// scoped to post-worktree-switch. For every other hook name the reserved
// code must stay an ordinary failure -- otherwise a hook that happens to
// exit 93 for unrelated reasons would be silently treated as success.
func TestHookExitCodes_ReservedCodeIsInertForOtherHooks(t *testing.T) {
	others := []string{
		"pre-worktree-switch",
		"post-worktree-add",
		"pre-worktree-add",
		"post-worktree-remove",
		"post-clone",
	}

	for _, hookName := range others {
		t.Run(hookName, func(t *testing.T) {
			worktreePath := t.TempDir()
			writeExecutableHook(t, worktreePath, hookName, exitingHook("x", ExitNavigationHandled))

			for variant, got := range runBothVariants(t, hookName, worktreePath) {
				if got.res.NavigationHandled {
					t.Errorf("%s: %s treated the reserved code as handled navigation", variant, hookName)
				}
				if got.err == nil {
					t.Errorf("%s: %s exiting %d should still be a failure",
						variant, hookName, ExitNavigationHandled)
				}
			}
		})
	}
}

// TestHookExitCodes_StdoutReachesUserOnHandledPath asserts that taking the
// handled-navigation branch does not cost the hook its voice. The hook's
// stdout is wired to the process's own stdout, so this test swaps
// os.Stdout for a pipe and reads back what the child wrote.
//
// Not parallel: it mutates the process-global os.Stdout.
func TestHookExitCodes_StdoutReachesUserOnHandledPath(t *testing.T) {
	worktreePath := t.TempDir()
	const marker = "hook-selected-the-tmux-window"
	writeExecutableHook(t, worktreePath, "post-worktree-switch", exitingHook(marker, ExitNavigationHandled))

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	runner := NewRunner(afero.NewOsFs())
	res, runErr := runner.ExecuteHookWithDetector(
		"post-worktree-switch", worktreePath, "github.com/test/repo", "feature-x", nil,
	)

	os.Stdout = origStdout
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	captured := string(buf[:n])
	if err := r.Close(); err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}

	if runErr != nil {
		t.Fatalf("handled-navigation run returned error: %v", runErr)
	}
	if !res.NavigationHandled {
		t.Fatalf("NavigationHandled = false, want true")
	}
	if !strings.Contains(captured, marker) {
		t.Errorf("hook stdout was swallowed on the handled path: captured %q, want it to contain %q",
			captured, marker)
	}
}

// TestExitNavigationHandled_DoesNotCollideWithPorcelainCodes pins the
// reserved value against the porcelain contract in CLAUDE.md. If someone
// later "tidies" the constant onto 1/128/129, this fails loudly.
func TestExitNavigationHandled_DoesNotCollideWithPorcelainCodes(t *testing.T) {
	reserved := map[int]string{
		0:   "success",
		1:   "operation failure",
		2:   "cobra usage error (main.go)",
		128: "fatal git/repo error",
		129: "usage error",
	}
	if name, clash := reserved[ExitNavigationHandled]; clash {
		t.Fatalf("ExitNavigationHandled = %d collides with the %q exit code", ExitNavigationHandled, name)
	}
	// 128+N is the shell's signal-termination band; staying below it keeps
	// the code distinguishable from "hook was killed by a signal".
	if ExitNavigationHandled >= 128 {
		t.Errorf("ExitNavigationHandled = %d falls in the 128+signal band", ExitNavigationHandled)
	}
	if ExitNavigationHandled < 0 || ExitNavigationHandled > 255 {
		t.Errorf("ExitNavigationHandled = %d is not a valid process exit status", ExitNavigationHandled)
	}
}
