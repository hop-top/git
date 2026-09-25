package hooks

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/output"
)

// mirrorPromptStderr runs a prompt-mode mirror of the named hooks reading
// stdin, and returns what went to stderr (warnings and hints) beside the
// result.
func mirrorPromptStderr(t *testing.T, stdin io.Reader, names ...string) (string, Result, error) {
	t.Helper()
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	for _, name := range names {
		writeHook(t, fs, "/wt", name, "#!/bin/sh\n", 0755)
	}
	var res Result
	var err error
	got := captureOSStderr(t, func() {
		output.SetupLogger(output.ModeHuman, false)
		res, err = MirrorCommittedHooks(fs, MirrorOpts{
			WorktreePath: "/wt",
			RepoID:       testRepoID,
			Mode:         ModePrompt,
			Stdin:        stdin,
			PromptOut:    &bytes.Buffer{},
		})
	})
	return got, res, err
}

// A prompt that gets no answer (stdin at end of input) is a warning that
// the hooks were not mirrored, with a hint naming the non-interactive way
// to mirror them, never git-hop's internal "prompt: EOF".
func TestMirror_PromptEOFWarnsNotMirrored(t *testing.T) {
	stderr, res, err := mirrorPromptStderr(t, strings.NewReader(""), "post-worktree-add")
	if err != nil {
		t.Fatalf("want no error on EOF, got %v", err)
	}
	if res.Installed != 0 || res.Skipped != 0 {
		t.Errorf("silence must not read as an answer, got %+v", res)
	}
	for _, want := range []string{
		"warning: the hooks prompt got no answer (end of input); committed hooks were not mirrored\n",
		"hint: Run 'git hop init --hooks=symlink' in /wt to mirror them.\n",
		"hint: Set 'git config hop.hooks.installMode symlink' to mirror them without asking.\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q lacks %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "EOF") {
		t.Errorf("stderr %q leaks the internal EOF", stderr)
	}
}

// Hooks answered before the input ran out keep their outcome; the
// warning speaks of the remaining ones.
func TestMirror_PromptEOFAfterAnswerWarnsRemaining(t *testing.T) {
	stderr, res, err := mirrorPromptStderr(t, strings.NewReader("n\n"), "post-worktree-add", "pre-worktree-add")
	if err != nil {
		t.Fatalf("want no error on EOF, got %v", err)
	}
	if res.Skipped != 1 || res.Installed != 0 {
		t.Errorf("want the answered hook skipped and nothing else, got %+v", res)
	}
	if want := "warning: the hooks prompt got no answer (end of input); the remaining committed hooks were not mirrored\n"; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q lacks %q", stderr, want)
	}
}

// failingReader fails every read with err.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// Any other read failure is still an error, with its current message.
func TestMirror_PromptReadErrorStaysError(t *testing.T) {
	boom := errors.New("boom")
	stderr, _, err := mirrorPromptStderr(t, failingReader{boom}, "post-worktree-add")
	if err == nil || err.Error() != "prompt: boom" || !errors.Is(err, boom) {
		t.Fatalf("want error %q, got %v", "prompt: boom", err)
	}
	if strings.Contains(stderr, "no answer") {
		t.Errorf("a read failure is not a missing answer: stderr %q", stderr)
	}
}
