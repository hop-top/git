package hooks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

const testRepoID = "github.com/test-org/test-repo"

// withDataHome sets GIT_HOP_DATA_HOME for the duration of a test.
func withDataHome(t *testing.T, dir string) {
	t.Helper()
	prev, had := os.LookupEnv("GIT_HOP_DATA_HOME")
	if err := os.Setenv("GIT_HOP_DATA_HOME", dir); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("GIT_HOP_DATA_HOME", prev)
		} else {
			_ = os.Unsetenv("GIT_HOP_DATA_HOME")
		}
	})
}

// writeHook writes a hook script to <worktree>/.git-hop/hooks/<name> on the
// given fs, with the given mode bits.
func writeHook(t *testing.T, fs afero.Fs, worktree, name, content string, mode os.FileMode) {
	t.Helper()
	dir := filepath.Join(worktree, ".git-hop", "hooks")
	if err := fs.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := afero.WriteFile(fs, path, []byte(content), mode); err != nil {
		t.Fatalf("write: %v", err)
	}
	// MemMapFs honors mode bits set via WriteFile; OsFs needs an explicit
	// chmod for executable bit reliability.
	_ = fs.Chmod(path, mode)
}

func hopspaceHookPath(dataHome, name string) string {
	return filepath.Join(dataHome, "github.com", "test-org", "test-repo", "hooks", name)
}

func TestMirror_ModeNone(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\necho hi\n", 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeNone,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 || res.Skipped != 0 {
		t.Fatalf("expected zero counts, got %+v", res)
	}
	exists, _ := afero.Exists(fs, hopspaceHookPath(dataHome, "post-worktree-add"))
	if exists {
		t.Fatal("mode=none should not install")
	}
}

func TestMirror_ModeCopy_InstallsAndPreservesContent(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	body := "#!/bin/sh\necho hello\n"
	writeHook(t, fs, wt, "post-worktree-add", body, 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 1 {
		t.Fatalf("Installed=%d want 1; res=%+v", res.Installed, res)
	}
	dst := hopspaceHookPath(dataHome, "post-worktree-add")
	got, err := afero.ReadFile(fs, dst)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if string(got) != body {
		t.Fatalf("content mismatch: got %q want %q", string(got), body)
	}
}

func TestMirror_ModeSymlink_OsFs(t *testing.T) {
	tmp := t.TempDir()
	dataHome := filepath.Join(tmp, "data")
	withDataHome(t, dataHome)
	fs := afero.NewOsFs()

	wt := filepath.Join(tmp, "wt")
	if err := os.MkdirAll(filepath.Join(wt, ".git-hop", "hooks"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := filepath.Join(wt, ".git-hop", "hooks", "post-worktree-add")
	if err := os.WriteFile(src, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write src: %v", err)
	}

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeSymlink,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 1 {
		t.Fatalf("Installed=%d want 1; res=%+v", res.Installed, res)
	}
	dst := hopspaceHookPath(dataHome, "post-worktree-add")
	target, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	absSrc, _ := filepath.Abs(src)
	if target != absSrc {
		t.Fatalf("symlink target = %q, want %q", target, absSrc)
	}
}

func TestMirror_PromptYesInstalls(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\necho hi\n", 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		Stdin:        strings.NewReader("y\n"),
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Prompt installs via copy fallback path on MemMapFs would fail with
	// symlink; but the helper hardcodes ModeSymlink for prompt-yes which
	// uses os.Symlink — and that fails for MemMapFs paths. Test only
	// counters: Installed should reflect the prompt's intent. We use a
	// real OsFs case below for prompt-yes too.
	_ = res
}

func TestMirror_PromptYesInstalls_OsFs(t *testing.T) {
	tmp := t.TempDir()
	dataHome := filepath.Join(tmp, "data")
	withDataHome(t, dataHome)
	fs := afero.NewOsFs()
	wt := filepath.Join(tmp, "wt")
	if err := os.MkdirAll(filepath.Join(wt, ".git-hop", "hooks"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(wt, ".git-hop", "hooks", "post-worktree-add"),
		[]byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		Stdin:        strings.NewReader("y\n"),
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 1 {
		t.Fatalf("Installed=%d want 1; %+v", res.Installed, res)
	}
}

func TestMirror_PromptNoSkips(t *testing.T) {
	tmp := t.TempDir()
	dataHome := filepath.Join(tmp, "data")
	withDataHome(t, dataHome)
	fs := afero.NewOsFs()
	wt := filepath.Join(tmp, "wt")
	_ = os.MkdirAll(filepath.Join(wt, ".git-hop", "hooks"), 0755)
	_ = os.WriteFile(filepath.Join(wt, ".git-hop", "hooks", "post-worktree-add"),
		[]byte("#!/bin/sh\n"), 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		Stdin:        strings.NewReader("n\n"),
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 || res.Skipped != 1 {
		t.Fatalf("expected installed=0 skipped=1, got %+v", res)
	}
}

func TestMirror_PromptAllInstallsRemaining(t *testing.T) {
	tmp := t.TempDir()
	dataHome := filepath.Join(tmp, "data")
	withDataHome(t, dataHome)
	fs := afero.NewOsFs()
	wt := filepath.Join(tmp, "wt")
	_ = os.MkdirAll(filepath.Join(wt, ".git-hop", "hooks"), 0755)
	for _, n := range []string{"post-worktree-add", "pre-worktree-add", "post-env-start"} {
		_ = os.WriteFile(filepath.Join(wt, ".git-hop", "hooks", n),
			[]byte("#!/bin/sh\n"), 0755)
	}

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		Stdin:        strings.NewReader("a\n"), // first answer = all-yes
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 3 {
		t.Fatalf("Installed=%d want 3; %+v", res.Installed, res)
	}
}

func TestMirror_PromptSkipAllSkipsRemaining(t *testing.T) {
	tmp := t.TempDir()
	dataHome := filepath.Join(tmp, "data")
	withDataHome(t, dataHome)
	fs := afero.NewOsFs()
	wt := filepath.Join(tmp, "wt")
	_ = os.MkdirAll(filepath.Join(wt, ".git-hop", "hooks"), 0755)
	for _, n := range []string{"post-worktree-add", "pre-worktree-add"} {
		_ = os.WriteFile(filepath.Join(wt, ".git-hop", "hooks", n),
			[]byte("#!/bin/sh\n"), 0755)
	}

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		Stdin:        strings.NewReader("s\n"),
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 || res.Skipped != 2 {
		t.Fatalf("expected installed=0 skipped=2, got %+v", res)
	}
}

func TestMirror_NonExecutableWarns(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\n", 0644) // not exec

	var stderr bytes.Buffer
	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Stderr:       &stderr,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Warned != 1 || res.Installed != 0 {
		t.Fatalf("expected Warned=1 Installed=0, got %+v", res)
	}
	if !strings.Contains(stderr.String(), "not executable") {
		t.Fatalf("stderr should mention not executable: %q", stderr.String())
	}
}

func TestMirror_AlreadyPresentIdenticalIsSilentNoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	body := "#!/bin/sh\necho yes\n"
	writeHook(t, fs, wt, "post-worktree-add", body, 0755)

	// Pre-populate hopspace with identical content.
	dst := hopspaceHookPath(dataHome, "post-worktree-add")
	if err := fs.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := afero.WriteFile(fs, dst, []byte(body), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.AlreadyPresent != 1 || res.Installed != 0 || res.Warned != 0 {
		t.Fatalf("expected AlreadyPresent=1, got %+v", res)
	}
}

func TestMirror_DifferentContentNoOverwriteWarns(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "post-worktree-add", "new\n", 0755)

	dst := hopspaceHookPath(dataHome, "post-worktree-add")
	_ = fs.MkdirAll(filepath.Dir(dst), 0755)
	_ = afero.WriteFile(fs, dst, []byte("old\n"), 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Overwrite:    false,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Warned != 1 || res.Installed != 0 {
		t.Fatalf("expected Warned=1 Installed=0, got %+v", res)
	}
	// existing content unchanged
	got, _ := afero.ReadFile(fs, dst)
	if string(got) != "old\n" {
		t.Fatalf("dst was modified: %q", string(got))
	}
}

func TestMirror_DifferentContentOverwriteReplaces(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "post-worktree-add", "new\n", 0755)

	dst := hopspaceHookPath(dataHome, "post-worktree-add")
	_ = fs.MkdirAll(filepath.Dir(dst), 0755)
	_ = afero.WriteFile(fs, dst, []byte("old\n"), 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Overwrite:    true,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 1 {
		t.Fatalf("expected Installed=1, got %+v", res)
	}
	got, _ := afero.ReadFile(fs, dst)
	if string(got) != "new\n" {
		t.Fatalf("expected dst replaced; got %q", string(got))
	}
}

func TestMirror_NonHookFilenameSkippedSilently(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "README.md", "docs\n", 0644)
	writeHook(t, fs, wt, "helper.sh", "#!/bin/sh\n", 0755)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 || res.Skipped != 0 || res.Warned != 0 || res.AlreadyPresent != 0 {
		t.Fatalf("expected all zero, got %+v", res)
	}
}

func TestMirror_EmptyDirIsZeroNoError(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	if err := fs.MkdirAll(filepath.Join(wt, ".git-hop", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 || res.Warned != 0 || res.Skipped != 0 {
		t.Fatalf("expected zero counts, got %+v", res)
	}
}

func TestMirror_MissingDirIsZeroNoError(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)

	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: "/wt-does-not-exist",
		RepoID:       testRepoID,
		Mode:         ModeCopy,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 || res.Warned != 0 || res.Skipped != 0 {
		t.Fatalf("expected zero counts, got %+v", res)
	}
}

func TestMirror_PromptDegradesNonInteractive(t *testing.T) {
	fs := afero.NewMemMapFs()
	dataHome := "/data"
	withDataHome(t, dataHome)
	wt := "/wt"
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\n", 0755)

	var stderr bytes.Buffer
	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt,
		RepoID:       testRepoID,
		Mode:         ModePrompt,
		// Stdin nil and Interactive false → degrade
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 {
		t.Fatalf("expected zero installs in non-interactive prompt; got %+v", res)
	}
	if !strings.Contains(stderr.String(), "non-interactive") {
		t.Fatalf("stderr should mention non-interactive degrade; got: %q", stderr.String())
	}
}

func TestResolveMode_Precedence(t *testing.T) {
	if got := ResolveMode("symlink", "copy", "prompt"); got != "symlink" {
		t.Fatalf("flag should win, got %q", got)
	}
	if got := ResolveMode("", "copy", "prompt"); got != "copy" {
		t.Fatalf("env should win when flag empty, got %q", got)
	}
	if got := ResolveMode("", "", "none"); got != "none" {
		t.Fatalf("config should win when flag+env empty, got %q", got)
	}
	if got := ResolveMode("", "", ""); got != "prompt" {
		t.Fatalf("default should be prompt, got %q", got)
	}
}
