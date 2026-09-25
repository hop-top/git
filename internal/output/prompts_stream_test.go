package output

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStreams returns what fn wrote to os.Stdout and os.Stderr.
func captureStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = prevOut, prevErr }()

	drain := func(r io.Reader, into chan<- string) {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		into <- buf.String()
	}
	outC, errC := make(chan string), make(chan string)
	go drain(outR, outC)
	go drain(errR, errC)

	fn()
	outW.Close()
	errW.Close()
	return <-outC, <-errC
}

// Prompt text is not a result: like git's own prompts it goes to stderr,
// so `git hop ... > file` captures only results and the question is still
// seen on the terminal.
func TestPrompts_WriteToStderrNotStdout(t *testing.T) {
	prompts := map[string]struct {
		input string
		run   func()
		want  []string
	}{
		"ConfirmAnswer": {"y\n", func() { _, _ = ConfirmAnswer("Continue?") }, []string{"Continue? (y/n): "}},
		"ConfirmDeletionAnswer": {"n\n", func() {
			_, _ = ConfirmDeletionAnswer("/tmp/x", []CardField{{Key: "Branch", Value: "feat"}})
		}, []string{"Confirm Removal", "/tmp/x", "warning: this action cannot be undone", "Continue? (y/n): "}},
		"ConfirmWithWarning": {"n\n", func() { ConfirmWithWarning("Delete everything", "details") },
			[]string{"Delete everything", "details", "Continue? (y/n): "}},
		"ChoiceAnswer": {"x\n1\n", func() { _, _ = ChoiceAnswer("Choose [1/2]: ", []string{"1", "2"}) },
			[]string{"Choose [1/2]: ", "Invalid choice"}},
		"Select": {"1\n", func() { Select("Pick one", []string{"a", "b"}) },
			[]string{"Pick one", "1) a", "Select option: "}},
		"Input":            {"v\n", func() { Input("Name") }, []string{"Name: "}},
		"InputWithDefault": {"\n", func() { InputWithDefault("Name", "d") }, []string{"Name", "[d]"}},
		"ConfirmWithPreview": {"n\n", func() { ConfirmWithPreview("Plan", []string{"step one"}) },
			[]string{"Plan", "step one", "Proceed? (y/n): "}},
	}
	for name, p := range prompts {
		t.Run(name, func(t *testing.T) {
			withMode(t, ModeHuman)
			withStdin(t, p.input)
			stdout, stderr := captureStreams(t, p.run)
			if stdout != "" {
				t.Errorf("stdout = %q, want empty: prompt text belongs on stderr", stdout)
			}
			for _, w := range p.want {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr %q lacks %q", stderr, w)
				}
			}
		})
	}
}

// The deletion banner is a warning, so it takes git's lowercase prefix.
func TestConfirmDeletion_BannerIsLowercaseWarning(t *testing.T) {
	withMode(t, ModeHuman)
	withStdin(t, "n\n")
	stdout, stderr := captureStreams(t, func() {
		_, _ = ConfirmDeletionAnswer("/tmp/x", nil)
	})
	if strings.Contains(stdout+stderr, "Warning:") {
		t.Errorf("banner uses capitalised Warning:; stdout %q stderr %q", stdout, stderr)
	}
}

// Modes that cannot prompt print nothing on either stream and report the
// prompt as unanswerable, exactly as before prompts moved to stderr.
func TestPrompts_NonHumanModesPrintNothing(t *testing.T) {
	for _, m := range []Mode{ModeQuiet, ModePorcelain, ModeJSON} {
		withMode(t, m)
		withStdin(t, "y\n")
		var err error
		stdout, stderr := captureStreams(t, func() {
			_, err = ConfirmDeletionAnswer("/tmp/x", nil)
		})
		if stdout != "" || stderr != "" {
			t.Errorf("mode %d: stdout %q stderr %q, want both empty", m, stdout, stderr)
		}
		if !isPromptUnanswerable(err) {
			t.Errorf("mode %d: err = %v, want ErrPromptUnanswerable", m, err)
		}
	}
}
