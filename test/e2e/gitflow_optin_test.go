package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitflowEnv is a hub whose repo is set up for git-flow-next (a feature/
// branch type), with a stub `git-flow` on PATH (see gitflowStub), and
// pre-worktree hooks that record the branch-type variables they receive.
type gitflowEnv struct {
	*TestEnv
	flowLog   string
	markerDir string
}

func setupGitflowEnv(t *testing.T) *gitflowEnv {
	t.Helper()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	binDir := filepath.Join(env.RootDir, "stub-bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	flowLog := filepath.Join(env.RootDir, "gitflow.log")
	WriteFile(t, filepath.Join(binDir, "git-flow"), strings.ReplaceAll(gitflowStub, "@LOG@", flowLog))
	if err := os.Chmod(filepath.Join(binDir, "git-flow"), 0755); err != nil {
		t.Fatal(err)
	}
	for i, kv := range env.EnvVars {
		if strings.HasPrefix(kv, "PATH=") {
			env.EnvVars[i] = "PATH=" + binDir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
		}
	}

	for key, value := range map[string]string{
		"gitflow.initialized":           "true",
		"gitflow.branch.feature.prefix": "feature/",
		"gitflow.branch.feature.parent": "main",
		"gitflow.branch.feature.type":   "topic",
	} {
		env.RunCommand(t, env.HubPath, "git", "config", key, value)
	}

	hooksDir := filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"pre-worktree-add", "pre-worktree-remove"} {
		script := "#!/bin/sh\nmkdir -p \"$GIT_HOP_TEST_MARKER_DIR\"\n" +
			"echo \"$GIT_HOP_DETECTOR_SOURCE $GIT_HOP_BRANCH_TYPE $GIT_HOP_BRANCH_NAME\" >> \"$GIT_HOP_TEST_MARKER_DIR/" + hook + "\"\n"
		WriteFile(t, filepath.Join(hooksDir, hook), script)
		if err := os.Chmod(filepath.Join(hooksDir, hook), 0755); err != nil {
			t.Fatal(err)
		}
	}

	return &gitflowEnv{TestEnv: env, flowLog: flowLog, markerDir: filepath.Join(env.RootDir, "markers")}
}

// gitflowStub stands in for git-flow-next as far as git hop relies on it.
// It appends each call's arguments to @LOG@ and, to @LOG@.where, the
// directory it ran in and the branch checked out there ("-" when
// detached). Like git-flow-next 2.1 it refuses to run outside a work tree
// (exit 3), and `start` creates <prefix><name> from its base (the [base]
// argument, else the type's parent) and checks it out where it runs.
// `finish` only records the call. A start fails while @LOG@.fail exists,
// and creates the branch without checking it out while @LOG@.nocheckout
// does.
const gitflowStub = `#!/bin/sh
echo "$*" >> '@LOG@'
echo "$(pwd -P) $(git branch --show-current 2>/dev/null | grep . || echo -)" >> '@LOG@.where'
if [ "$(git rev-parse --is-inside-work-tree 2>/dev/null)" != true ]; then
	echo "Error: failed to open repository: $(pwd) is not inside a git work tree" >&2
	exit 3
fi
if [ "$2" = start ]; then
	[ -e '@LOG@.fail' ] && { echo "Error: start failed" >&2; exit 1; }
	prefix=$(git config "gitflow.branch.$1.prefix")
	base=$4
	case "$base" in ''|-*) base=$(git config "gitflow.branch.$1.parent") ;; esac
	if [ -e '@LOG@.nocheckout' ]; then
		git branch "$prefix$3" "$base" || exit 1
	else
		git checkout -q -b "$prefix$3" "$base" || exit 1
	fi
	git config "gitflow.branch.$prefix$3.base" "$base"
fi
`

// flowWhere returns, per recorded git-flow call, the directory it ran in
// and the branch checked out there, as "<dir> <branch>".
func (e *gitflowEnv) flowWhere(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.flowLog + ".where")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (e *gitflowEnv) enable(t *testing.T) {
	t.Helper()
	e.RunCommand(t, e.HubPath, "git", "config", "hop.gitflow.enabled", "true")
}

// flowCalls returns the git-flow invocations the stub recorded.
func (e *gitflowEnv) flowCalls(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.flowLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (e *gitflowEnv) hookSaw(t *testing.T, hook string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.markerDir, hook))
	if err != nil {
		t.Fatalf("%s did not run: %v", hook, err)
	}
	return strings.TrimSpace(string(data))
}

// gitHop runs git-hop in the hub and fails the test on a non-zero exit.
func (e *gitflowEnv) gitHop(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := e.RunCommandWithExit(t, e.HubPath, e.BinPath, args...)
	if code != 0 {
		t.Fatalf("git-hop %v exited %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout, stderr
}

func countHints(stderr string) int {
	n := 0
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "hint:") && strings.Contains(line, "hop.gitflow.enabled") {
			n++
		}
	}
	return n
}

// By default git-hop leaves git-flow alone: add and remove run no git-flow
// command, yet the branch type is still detected for the hooks.
func TestGitflowOptIn_DefaultRunsNoGitflow(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)

	_, addErr := e.gitHop(t, "add", "feature/x")
	_, rmErr := e.gitHop(t, "remove", "feature/x", "--no-prompt")

	if calls := e.flowCalls(t); len(calls) != 0 {
		t.Errorf("git-flow ran without hop.gitflow.enabled: %v", calls)
	}
	for _, hook := range []string{"pre-worktree-add", "pre-worktree-remove"} {
		if got, want := e.hookSaw(t, hook), "gitflow-next feature x"; got != want {
			t.Errorf("%s saw detector vars %q, want %q", hook, got, want)
		}
	}
	if n := countHints(addErr + rmErr); n != 0 {
		t.Errorf("hint shown without --verbose:\n%s%s", addErr, rmErr)
	}
}

// Opting in restores the git-flow start/finish calls.
func TestGitflowOptIn_EnabledRunsStartAndFinish(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)

	_, addErr := e.gitHop(t, "add", "feature/x", "--verbose")
	_, rmErr := e.gitHop(t, "remove", "feature/x", "--no-prompt", "--verbose")

	want := []string{"feature start x --no-worktree", "feature finish x"}
	if got := e.flowCalls(t); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("git-flow calls = %q, want %q", got, want)
	}
	if got := e.hookSaw(t, "pre-worktree-add"); got != "gitflow-next feature x" {
		t.Errorf("pre-worktree-add saw %q", got)
	}
	if n := countHints(addErr + rmErr); n != 0 {
		t.Errorf("opt-in hint shown although enabled:\n%s%s", addErr, rmErr)
	}
}

// Under --verbose, a command that skips git-flow says how to opt in, once.
func TestGitflowOptIn_VerboseHintOncePerCommand(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)

	_, addErr := e.gitHop(t, "add", "feature/a", "--verbose")
	if n := countHints(addErr); n != 1 {
		t.Errorf("add --verbose printed %d opt-in hints, want 1:\n%s", n, addErr)
	}
	e.gitHop(t, "add", "feature/b")

	// One command skipping git-flow for two branches still hints once.
	_, rmErr := e.gitHop(t, "remove", "--merged", "--no-prompt", "--verbose")
	if n := countHints(rmErr); n != 1 {
		t.Errorf("remove --merged --verbose printed %d opt-in hints, want 1:\n%s", n, rmErr)
	}
	if calls := e.flowCalls(t); len(calls) != 0 {
		t.Errorf("git-flow ran without hop.gitflow.enabled: %v", calls)
	}

	// Branches no git-flow type matches have nothing to skip.
	_, plainErr := e.gitHop(t, "add", "plain-branch", "--verbose")
	if n := countHints(plainErr); n != 0 {
		t.Errorf("hint shown for a non-git-flow branch:\n%s", plainErr)
	}
}

// Previews name the git-flow command only when a real run would run it.
func TestGitflowOptIn_DryRunMatrix(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		name := "default"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := setupGitflowEnv(t)
			if enabled {
				e.enable(t)
			}
			e.gitHop(t, "add", "feature/x")
			before := len(e.flowCalls(t))

			cases := []struct {
				args []string
				line string
			}{
				{[]string{"add", "feature/y", "-n"}, "[dry-run] Would run 'git flow feature start y'"},
				{[]string{"remove", "feature/x", "-n", "--no-prompt"}, "[dry-run] Would run 'git flow feature finish x'"},
			}
			for _, c := range cases {
				out, _ := e.gitHop(t, c.args...)
				if got := strings.Contains(out, c.line); got != enabled {
					t.Errorf("git-hop %v: preview has %q = %v, want %v\n%s", c.args, c.line, got, enabled, out)
				}
			}
			if after := len(e.flowCalls(t)); after != before {
				t.Errorf("dry-run invoked git-flow: %v", e.flowCalls(t)[before:])
			}
		})
	}
}
