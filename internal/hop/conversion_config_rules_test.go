package hop

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
)

// TestExcludedConfigRules pins the documented list of local config keys
// a bare conversion leaves out, and the keys it deliberately carries
// although git init or clone writes them as well.
func TestExcludedConfigRules(t *testing.T) {
	cases := []struct {
		key, value string
		excluded   bool
	}{
		// the old layout and storage format
		{"core.bare", "false", true},
		{"core.worktree", "/src/proj", true},
		{"core.repositoryformatversion", "1", true},
		{"extensions.worktreeconfig", "true", true},
		{"extensions.objectformat", "sha256", true},
		{"extensions.partialclone", "origin", true},
		// includes whose meaning depends on where the config file lives
		{"include.path", "../shared.gitconfig", true},
		{"include.path", "shared.gitconfig", true},
		{"includeif.onbranch:main.path", "../main.gitconfig", true},
		{"includeif.gitdir:./.path", "/abs/x.gitconfig", true},
		{"includeif.gitdir/i:./sub/.path", "~/x.gitconfig", true},
		// includes that resolve the same from the hub
		{"include.path", "/etc/shared.gitconfig", false},
		{"include.path", "~/shared.gitconfig", false},
		{"includeif.onbranch:main.path", "/abs/main.gitconfig", false},
		{"includeif.gitdir:~/work/.path", "~/work.gitconfig", false},
		// filesystem probes and init defaults: same filesystem, user wins
		{"core.filemode", "false", false},
		{"core.ignorecase", "true", false},
		{"core.precomposeunicode", "true", false},
		{"core.symlinks", "false", false},
		{"core.logallrefupdates", "true", false},
		// user settings
		{"core.hookspath", ".githooks", false},
		{"core.sparsecheckout", "true", false},
		{"user.email", "me@example.test", false},
		{"url.https://mirror.test/.insteadof", "https://git.test/", false},
		{"alias.st", "status", false},
		{"hop.backup.maxbackups", "7", false},
		{"commit.gpgsign", "true", false},
		{"gc.auto", "0", false},
		{"submodule.lib.url", "../lib.git", false},
		{"submodule.lib.active", "true", false},
		// a relative value is fine outside an include
		{"core.excludesfile", "../ignore", false},
		// owned by the remotes step: carried, not excluded
		{"remote.origin.url", "https://git.test/a/b.git", false},
		{"branch.main.merge", "refs/heads/main", false},
	}
	for _, tc := range cases {
		reason := excludedReason(tc.key, tc.value)
		if got := reason != ""; got != tc.excluded {
			t.Errorf("%s=%s: excluded = %v (reason %q), want %v", tc.key, tc.value, got, reason, tc.excluded)
		}
	}
}

// recordingRunner runs real git and records every command line.
type recordingRunner struct {
	git.RealRunner
	calls [][]string
}

func (r *recordingRunner) Run(cmd string, args ...string) (string, error) {
	r.calls = append(r.calls, args)
	return r.RealRunner.Run(cmd, args...)
}

// TestCarryOverLocalConfig_LeavesRemotesToTheRemotesStep: remote.* and
// branch.* are written by carryOverRemotes; the general copy never
// touches them, not even to replace them with the same values.
func TestCarryOverLocalConfig_LeavesRemotesToTheRemotesStep(t *testing.T) {
	dir := t.TempDir()
	src, hub := filepath.Join(dir, "src"), filepath.Join(dir, "hub")
	for _, args := range [][]string{
		{"init", "-b", "main", src},
		{"-C", src, "commit", "--allow-empty", "-m", "init"},
		{"-C", src, "remote", "add", "origin", "https://git.example.test/a/b.git"},
		{"-C", src, "config", "branch.main.remote", "origin"},
		{"-C", src, "config", "user.email", "local@example.test"},
		{"clone", "--bare", "--quiet", src, hub},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	rec := &recordingRunner{}
	c := NewConverter(afero.NewOsFs(), git.New(git.WithRunner(rec)))
	plan, err := PlanLocalConfig(c.git, src)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.carryOverLocalConfig(plan, hub); err != nil {
		t.Fatal(err)
	}

	wroteUser := false
	for _, args := range rec.calls {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, " config --add ") && !strings.Contains(joined, " config --unset-all ") {
			continue
		}
		key := args[len(args)-1]
		if strings.Contains(joined, "--add") {
			key = args[len(args)-2]
		}
		if isRemoteConfig(key) {
			t.Errorf("general copy wrote %s: %v", key, args)
		}
		wroteUser = wroteUser || key == "user.email"
	}
	if !wroteUser {
		t.Errorf("user.email not written; calls: %v", rec.calls)
	}
}
