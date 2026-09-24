package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cwdRecorderHook records the physical working directory a hook starts
// in. /bin/pwd -P asks the kernel, so a directory that no longer exists
// shows up as a failure rather than as a stale $PWD.
const cwdRecorderHook = `#!/bin/bash
mkdir -p "$GIT_HOP_TEST_MARKER_DIR"
dir=$(/bin/pwd -P 2>/dev/null)
echo "rc=$?" > "$GIT_HOP_TEST_MARKER_DIR/hook-cwd.log"
echo "cwd=$dir" >> "$GIT_HOP_TEST_MARKER_DIR/hook-cwd.log"
`

// A bare conversion replaces the directory init was started in with the
// new hub. The hook it fires afterwards must start in a directory that
// exists (the hub, where the user ran init, as clone's hooks start where
// clone was run), and bash must not report a vanished cwd on stderr.
func TestInitLifecycleHooks_BareConversionHookCwdExists(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath := seedPlainRepo(t, env, "proj", "main")

	hook := filepath.Join(globalHooksDir(env), "post-worktree-add")
	if err := os.MkdirAll(filepath.Dir(hook), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	WriteFile(t, hook, cwdRecorderHook)
	if err := os.Chmod(hook, 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, stderr, code := env.RunCommandWithExit(t, repoPath, env.BinPath, "init", "--no-prompt")
	if code != 0 {
		t.Fatalf("init exited %d:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "getcwd") {
		t.Errorf("init stderr reports a vanished working directory:\n%s", stderr)
	}

	data, err := os.ReadFile(filepath.Join(env.RootDir, "markers", "hook-cwd.log"))
	if err != nil {
		t.Fatalf("post-worktree-add did not run: %v", err)
	}
	rec := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			rec[k] = v
		}
	}
	if rec["rc"] != "0" || rec["cwd"] == "" {
		t.Fatalf("hook could not resolve its working directory: %q", string(data))
	}
	if info, err := os.Stat(rec["cwd"]); err != nil || !info.IsDir() {
		t.Fatalf("hook cwd %q is not an existing directory: %v", rec["cwd"], err)
	}
	if !samePath(t, rec["cwd"], repoPath) {
		t.Errorf("hook cwd = %q; want the hub %q", rec["cwd"], repoPath)
	}
}
