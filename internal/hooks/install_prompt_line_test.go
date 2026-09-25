package hooks

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// installPrompt is how the install prompt for a new hook ends.
const installPrompt = "(y=yes, n=no, a=all-yes, s=skip-all): "

// mirrorPrompt runs a prompt-mode mirror of the named hooks with stdin
// holding answer, and returns what the prompt wrote.
func mirrorPrompt(t *testing.T, answer string, names ...string) (string, Result, error) {
	t.Helper()
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	for _, name := range names {
		writeHook(t, fs, "/wt", name, "#!/bin/sh\n", 0755)
	}
	var out bytes.Buffer
	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: "/wt",
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		Stdin:        strings.NewReader(answer),
		PromptOut:    &out,
	})
	return out.String(), res, err
}

// A piped answer is never echoed, so the prompt closes its own line:
// what is printed next starts at column zero, not after the prompt.
func TestMirror_PromptPipedAnswerEndsLine(t *testing.T) {
	for _, answer := range []string{"n\n", "n"} {
		out, res, err := mirrorPrompt(t, answer, "post-worktree-add")
		if err != nil {
			t.Fatalf("answer %q: %v", answer, err)
		}
		if res.Skipped != 1 {
			t.Errorf("answer %q: want the hook skipped, got %+v", answer, res)
		}
		if want := installPrompt + "\n"; !strings.HasSuffix(out, want) {
			t.Errorf("answer %q: output %q does not end with %q", answer, out, want)
		}
	}
}

// The next hook's prompt starts on its own line.
func TestMirror_PromptPipedAnswersEachOnTheirOwnLine(t *testing.T) {
	out, _, err := mirrorPrompt(t, "n\nn\n", "post-worktree-add", "pre-worktree-add")
	if err != nil {
		t.Fatal(err)
	}
	if want := installPrompt + "\n--- pre-worktree-add ---\n"; !strings.Contains(out, want) {
		t.Errorf("output %q does not contain %q", out, want)
	}
}

// A piped invalid choice is answered on its own line.
func TestMirror_PromptPipedRetryOnItsOwnLine(t *testing.T) {
	out, _, err := mirrorPrompt(t, "x\nn\n", "post-worktree-add")
	if err != nil {
		t.Fatal(err)
	}
	if want := installPrompt + "\nInvalid choice."; !strings.Contains(out, want) {
		t.Errorf("output %q does not contain %q", out, want)
	}
}

// With no answer at all (EOF) the line is closed once, and the mirror
// stops instead of reading the silence as an answer (the warning is
// pinned in install_prompt_eof_test.go).
func TestMirror_PromptEOFEndsLine(t *testing.T) {
	out, res, err := mirrorPrompt(t, "", "post-worktree-add")
	if err != nil || res.Installed != 0 || res.Skipped != 0 {
		t.Errorf("want the mirror stopped with nothing answered, got %+v, %v", res, err)
	}
	if want := installPrompt + "\n"; !strings.HasSuffix(out, want) || strings.HasSuffix(out, "\n\n") {
		t.Errorf("output %q does not end with exactly %q", out, want)
	}
}
