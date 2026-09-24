package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/output"
)

// mirrorStderr runs a copy-mode mirror of one non-executable hook in
// mode, with os.Stderr captured, and returns what it wrote there.
func mirrorStderr(t *testing.T, mode output.Mode) string {
	t.Helper()
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	writeHook(t, fs, "/wt", "post-worktree-add", "#!/bin/sh\n", 0644)

	var res Result
	var err error
	got := captureOSStderr(t, func() {
		output.SetupLogger(mode, false)
		res, err = MirrorCommittedHooks(fs, MirrorOpts{
			WorktreePath: "/wt",
			RepoID:       testRepoID,
			Mode:         ModeCopy,
		})
	})
	output.SetupLogger(output.ModeHuman, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Warned != 1 {
		t.Fatalf("expected Warned=1, got %+v", res)
	}
	return got
}

// captureOSStderr runs fn with os.Stderr redirected and returns what it
// wrote. A logger set up inside fn binds to the redirected stream.
func captureOSStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// Per-hook warnings go through the output package, so -q drops them and
// JSON mode renders them as records, like every other warning.
func TestMirror_WarningsFollowOutputMode(t *testing.T) {
	human := mirrorStderr(t, output.ModeHuman)
	if !strings.HasPrefix(human, "warning: hook post-worktree-add is not executable") {
		t.Errorf("human stderr = %q, want a warning: line", human)
	}

	if quiet := mirrorStderr(t, output.ModeQuiet); quiet != "" {
		t.Errorf("-q stderr = %q, want empty", quiet)
	}

	got := mirrorStderr(t, output.ModeJSON)
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &rec); err != nil {
		t.Fatalf("JSON-mode stderr is not one JSON record: %q (%v)", got, err)
	}
	if rec["level"] != "warn" || !strings.Contains(rec["msg"].(string), "not executable") {
		t.Errorf("JSON record = %v, want level=warn about the non-executable hook", rec)
	}
}
