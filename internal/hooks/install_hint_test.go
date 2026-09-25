package hooks

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/output"
)

// mirrorAdvice runs a mirror of /wt in mode with os.Stderr captured under
// the given output mode, and returns what it wrote there. withHook seeds
// a committed hook (executable when exec is true).
func mirrorAdvice(t *testing.T, outMode output.Mode, mode string, withHook, exec bool) string {
	t.Helper()
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	if withHook {
		perm := os.FileMode(0644)
		if exec {
			perm = 0755
		}
		writeHook(t, fs, "/wt", "post-worktree-add", "#!/bin/sh\n", perm)
	}
	var err error
	got := captureOSStderr(t, func() {
		output.SetupLogger(outMode, false)
		_, err = MirrorCommittedHooks(fs, MirrorOpts{
			WorktreePath: "/wt",
			RepoID:       testRepoID,
			Mode:         mode,
		})
	})
	output.SetupLogger(output.ModeHuman, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	return got
}

// assertAllHint fails unless every line of got is a git-style hint: line.
func assertAllHint(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Fatal("stderr is empty, want hint: advice")
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if !strings.HasPrefix(line, "hint: ") {
			t.Errorf("line %q is not a hint: line (stderr %q)", line, got)
		}
	}
}

// Skipping the mirror is advice on how to get the hooks mirrored, so it
// is a hint: naming the command that mirrors them in place.
func TestMirror_SkipAdviceIsHint(t *testing.T) {
	cases := map[string]string{
		"non-interactive prompt": ModePrompt,
		"none":                   ModeNone,
	}
	for name, mode := range cases {
		t.Run(name, func(t *testing.T) {
			got := mirrorAdvice(t, output.ModeHuman, mode, true, true)
			assertAllHint(t, got)
			if !strings.Contains(got, "git hop init --hooks=symlink") || !strings.Contains(got, "/wt") {
				t.Errorf("hint %q should name 'git hop init --hooks=symlink' and the worktree", got)
			}
		})
	}
}

// -q drops the advice, as it drops every hint.
func TestMirror_SkipAdviceQuiet(t *testing.T) {
	for _, mode := range []string{ModePrompt, ModeNone} {
		if got := mirrorAdvice(t, output.ModeQuiet, mode, true, true); got != "" {
			t.Errorf("mode %s: -q stderr = %q, want empty", mode, got)
		}
	}
}

// A repo that commits no hooks has nothing to mirror, so there is
// nothing to advise about.
func TestMirror_SkipAdviceSilentWithoutCommittedHooks(t *testing.T) {
	for _, mode := range []string{ModePrompt, ModeNone} {
		if got := mirrorAdvice(t, output.ModeHuman, mode, false, false); got != "" {
			t.Errorf("mode %s: stderr = %q, want empty (no committed hooks)", mode, got)
		}
	}
}

// A non-executable hook warns, then advises a remedy that exists: there
// is no "hooks sync" command; re-running init in the worktree mirrors.
func TestMirror_NonExecutableAdviceIsHint(t *testing.T) {
	got := mirrorAdvice(t, output.ModeHuman, ModeCopy, true, false)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("stderr = %q, want a warning: then hint: lines", got)
	}
	if lines[0] != "warning: hook post-worktree-add is not executable; skipping" {
		t.Errorf("warning line = %q", lines[0])
	}
	assertAllHint(t, strings.Join(lines[1:], "\n")+"\n")
	if strings.Contains(got, "hooks sync") || strings.Contains(got, "TBD") {
		t.Errorf("stderr %q still points at the nonexistent 'git hop hooks sync'", got)
	}
	if !strings.Contains(got, "chmod +x") || !strings.Contains(got, "git hop init --hooks=copy") {
		t.Errorf("hint %q should say chmod +x and 'git hop init --hooks=copy'", got)
	}
}

// The per-hook install prompt is prompt text, not a result: with no
// writer given it goes to stderr, never stdout.
func TestMirror_PromptDefaultsToStderr(t *testing.T) {
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	writeHook(t, fs, "/wt", "post-worktree-add", "#!/bin/sh\n", 0755)

	var err error
	stdout := captureOSStdout(t, func() {
		stderr := captureOSStderr(t, func() {
			_, err = MirrorCommittedHooks(fs, MirrorOpts{
				WorktreePath: "/wt",
				RepoID:       testRepoID,
				Mode:         ModePrompt,
				Stdin:        strings.NewReader("n\n"),
				Interactive:  true,
			})
		})
		if !strings.Contains(stderr, "Install hook post-worktree-add?") {
			t.Errorf("stderr %q lacks the install prompt", stderr)
		}
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty: the prompt belongs on stderr", stdout)
	}
}

// captureOSStdout runs fn with os.Stdout redirected and returns what it
// wrote.
func captureOSStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	fn()
	os.Stdout = old
	w.Close()
	return <-done
}
