package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const miseTOML = "[env]\nHOP_E2E = \"1\"\n"

// setupMiseHub is a hub whose repository commits a mise.toml that needs
// trust (an [env] section), with mise's trust state kept in the test's
// own directories. It skips when mise is not installed.
func setupMiseHub(t *testing.T) (*TestEnv, string) {
	t.Helper()
	if _, err := exec.LookPath("mise"); err != nil {
		t.Skip("mise not installed")
	}
	env := SetupTestEnv(t)
	misedir := filepath.Join(env.RootDir, "mise")
	env.EnvVars = append(env.EnvVars,
		"MISE_DATA_DIR="+filepath.Join(misedir, "data"),
		"MISE_STATE_DIR="+filepath.Join(misedir, "state"),
		"MISE_CONFIG_DIR="+filepath.Join(misedir, "config"),
		"MISE_CACHE_DIR="+filepath.Join(misedir, "cache"),
		"MISE_GLOBAL_CONFIG_FILE="+filepath.Join(misedir, "config", "config.toml"),
	)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	WriteFile(t, filepath.Join(env.SeedRepoPath, "mise.toml"), miseTOML)
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "mise.toml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "mise config")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	return env, filepath.Join(env.HubPath, "hops", "main")
}

// miseTrustState is mise's verdict on dir's config: trusted or untrusted.
func miseTrustState(t *testing.T, env *TestEnv, dir string) string {
	t.Helper()
	out := env.RunCommand(t, env.RootDir, "mise", "-C", dir, "trust", "--show")
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		i := strings.LastIndex(line, ": ")
		if i <= 0 {
			continue
		}
		if got, err := filepath.EvalSymlinks(line[:i]); err == nil && got == want {
			return line[i+2:]
		}
	}
	t.Fatalf("mise trust --show has no line for %s:\n%s", dir, out)
	return ""
}

func TestAddMiseTrust_TrustedSourceTrustsNewWorktree(t *testing.T) {
	t.Parallel()
	env, main := setupMiseHub(t)
	env.RunCommand(t, env.RootDir, "mise", "trust", main)

	out := env.RunGitHopCombined(t, env.HubPath, "add", "feat/x", "--dry-run")
	if !strings.Contains(out, "[dry-run] Would trust the new worktree's mise config if it matches the trusted config of") {
		t.Errorf("dry-run does not report the trust:\n%s", out)
	}

	out = env.RunGitHopCombined(t, env.HubPath, "add", "feat/x")
	if !strings.Contains(out, "Trusted mise config in") {
		t.Errorf("add does not report the trust:\n%s", out)
	}
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "x")); got != "trusted" {
		t.Errorf("new worktree is %s, want trusted", got)
	}
}

func TestAddMiseTrust_UntrustedSourceLeavesNewWorktree(t *testing.T) {
	t.Parallel()
	env, _ := setupMiseHub(t)
	out := env.RunGitHopCombined(t, env.HubPath, "add", "feat/x")
	if !strings.Contains(out, "hint: not trusting the new worktree's mise config") {
		t.Errorf("no hint for the untrusted source:\n%s", out)
	}
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "x")); got != "untrusted" {
		t.Errorf("new worktree is %s, want untrusted", got)
	}
}

func TestAddMiseTrust_ConfigDiffersLeavesNewWorktree(t *testing.T) {
	t.Parallel()
	env, main := setupMiseHub(t)
	// The source's config, trusted, no longer matches the committed one.
	WriteFile(t, filepath.Join(main, "mise.toml"), miseTOML+"EXTRA = \"1\"\n")
	env.RunCommand(t, env.RootDir, "mise", "trust", main)

	out := env.RunGitHopCombined(t, main, "add", "feat/x")
	if !strings.Contains(out, "mise.toml differs from the source's") {
		t.Errorf("no hint for the differing config:\n%s", out)
	}
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "x")); got != "untrusted" {
		t.Errorf("new worktree is %s, want untrusted", got)
	}
}

func TestAddMiseTrust_NeverAndAlways(t *testing.T) {
	t.Parallel()
	env, main := setupMiseHub(t)
	env.RunCommand(t, env.RootDir, "mise", "trust", main)

	env.RunCommand(t, env.HubPath, "git", "config", "hop.add.miseTrust", "never")
	out := env.RunGitHopCombined(t, env.HubPath, "add", "feat/never")
	if strings.Contains(out, "mise") {
		t.Errorf("never: add mentions mise:\n%s", out)
	}
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "never")); got != "untrusted" {
		t.Errorf("never: new worktree is %s, want untrusted", got)
	}

	// --mise-trust beats the config for one run.
	env.RunGitHop(t, env.HubPath, "add", "feat/flag", "--mise-trust")
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "flag")); got != "trusted" {
		t.Errorf("--mise-trust: new worktree is %s, want trusted", got)
	}

	// always trusts without a trusted source.
	env.RunCommand(t, env.RootDir, "mise", "trust", "--untrust", main)
	env.RunCommand(t, env.HubPath, "git", "config", "hop.add.miseTrust", "always")
	env.RunGitHop(t, env.HubPath, "add", "feat/always")
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "always")); got != "trusted" {
		t.Errorf("always: new worktree is %s, want trusted", got)
	}

	// --no-mise-trust beats always.
	env.RunGitHop(t, env.HubPath, "add", "feat/noflag", "--no-mise-trust")
	if got := miseTrustState(t, env, filepath.Join(env.HubPath, "hops", "feat", "noflag")); got != "untrusted" {
		t.Errorf("--no-mise-trust: new worktree is %s, want untrusted", got)
	}
}

// Without mise on PATH, add skips the trust silently.
func TestAddMiseTrust_MiseMissing(t *testing.T) {
	t.Parallel()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	env := SetupTestEnv(t)
	bin := filepath.Join(env.RootDir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	env.RunCommand(t, env.RootDir, "ln", "-s", gitPath, filepath.Join(bin, "git"))
	for _, d := range []string{"/usr/bin", "/bin"} {
		if _, err := os.Stat(filepath.Join(d, "mise")); err == nil {
			t.Skip("mise is installed in " + d)
		}
	}
	for i, v := range env.EnvVars {
		if strings.HasPrefix(v, "PATH=") {
			env.EnvVars[i] = "PATH=" + bin + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"
		}
	}
	env.RunCommand(t, env.RootDir, gitPath, "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, gitPath, "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, gitPath, "commit", "--allow-empty", "-m", "init")
	env.RunCommand(t, env.SeedRepoPath, gitPath, "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// git-hop does not need mise: without it, add says nothing about it.
	for _, args := range [][]string{{"add", "feat/x", "--dry-run"}, {"add", "feat/x"}} {
		if out := env.RunGitHopCombined(t, env.HubPath, args...); strings.Contains(out, "mise") {
			t.Errorf("%v mentions mise without mise installed:\n%s", args, out)
		}
	}
}

// With mise installed, a repository without mise config gets no word
// about it either.
func TestAddMiseTrust_NoMiseConfigIsSilent(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("mise"); err != nil {
		t.Skip("mise not installed")
	}
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "init")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	for _, args := range [][]string{{"add", "feat/x", "--dry-run"}, {"add", "feat/x"}} {
		if out := env.RunGitHopCombined(t, env.HubPath, args...); strings.Contains(out, "mise") {
			t.Errorf("%v mentions mise in a repository without mise config:\n%s", args, out)
		}
	}
}
