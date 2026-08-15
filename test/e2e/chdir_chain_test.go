package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hop.top/git/internal/hooks"
	"hop.top/git/internal/shell"
)

// The binary and the generated shell integration are each covered alone.
// internal/shell runs the real interpreters against a STUB git that logs
// argv; test/e2e/switch_nav_directive_test.go runs the real binary but with
// no shell wrapper around it. Between them sits the thing users actually
// have: a real shell that sourced the real integration and shells out to
// the real binary, which reads a real hop.json and fires a real hook.
//
// Everything in this file composes those two halves. The stub git is gone
// -- the `git` these shells find on PATH forwards `hop` to the compiled
// binary and everything else to the system git -- so a defect anywhere in
// the chain (generator, wrapper, chdir handler, notify subcommand, hook
// runner, exit-status plumbing) surfaces as the shell ending up in the
// wrong directory or the hook log being wrong.

// chainShells are the interpreters the integration is generated for. A
// missing one is skipped rather than failed: not every machine running the
// suite has fish, and a skip says "unproven here" where a failure would say
// "broken", which is the wrong claim.
var chainShells = []struct{ bin, shellType string }{
	{"bash", "bash"},
	{"zsh", "zsh"},
	{"fish", "fish"},
}

// lookChainShell resolves an interpreter or skips the subtest.
func lookChainShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	return path
}

// The generated integration embeds the roots-cache path resolved at
// GENERATION time, from the generating process's own environment. The e2e
// tests run in parallel and thread their environment explicitly (see the
// note in SetupTestEnv), so the parent test process cannot set
// XDG_CACHE_HOME for itself without leaking into every other running test
// -- and t.Setenv is outright incompatible with t.Parallel.
//
// So generation happens in a CHILD of the test binary, re-executed with
// this test's environment. The child runs the same generator the install
// path calls, which is what keeps this an assertion about the shipped
// block rather than about a hand-assembled copy of it.
const (
	genEnvVar   = "GIT_HOP_E2E_GENERATE_SHELL"
	genTestName = "TestChdirChainHelperGenerate"
)

// TestChdirChainHelperGenerate is not a test. It is the entry point of the
// re-executed child described above: when the marker variable names a shell
// type it prints that shell's integration block and exits, and otherwise it
// is an ordinary no-op the suite runs and ignores.
func TestChdirChainHelperGenerate(t *testing.T) {
	shellType := os.Getenv(genEnvVar)
	if shellType == "" {
		t.Skip("helper process entry point; not a test")
	}
	os.Stdout.WriteString(shell.GenerateWrapperFunction(shellType))
	os.Exit(0)
}

// chainEnv is a test environment with a hub, a shell integration block
// generated against it, and a `git` shim that reaches the real binary.
type chainEnv struct {
	*TestEnv
	CacheHome  string
	MarkerDir  string
	BinDir     string
	HooksDir   string
	shellEnv   []string
	MainTree   string
	scriptRoot string
}

// setupChainEnv builds the hub, the hook directory, the git shim, and the
// per-test cache home, and returns an environment whose EnvVars every child
// -- shell, shim, binary, hook -- inherits.
//
// XDG_CACHE_HOME is added here rather than in SetupTestEnv because it is
// this file's chain that depends on it: the binary writes the roots cache
// under it and the generated handler reads the same path back. Left unset,
// both would land on the developer's real cache and the test would be
// asserting against whatever their machine happens to hold.
func setupChainEnv(t *testing.T) *chainEnv {
	t.Helper()

	env := SetupTestEnv(t)

	// Resolve the root before deriving paths from it. On macOS the temp
	// root is under /var, a symlink to /private/var, so a shell reports
	// the resolved spelling in $PWD while the test holds the unresolved
	// one -- two names for one directory, and a comparison that fails for
	// reasons that say nothing about the chain.
	if resolved, err := filepath.EvalSymlinks(env.RootDir); err == nil {
		env.RootDir = resolved
		env.HubPath = filepath.Join(resolved, "hub")
		env.BareRepoPath = filepath.Join(resolved, "repo.git")
		env.SeedRepoPath = filepath.Join(resolved, "seed")
	}

	cacheHome := filepath.Join(env.RootDir, ".cache")
	markerDir := filepath.Join(env.RootDir, "markers")
	binDir := filepath.Join(env.RootDir, "shimbin")
	for _, d := range []string{cacheHome, markerDir, binDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// EnvVars carries GIT_HOP_TEST_MARKER_DIR and the XDG trio already;
	// only the cache home is missing, and it is appended rather than
	// rewritten so nothing else in the harness shifts underneath.
	env.EnvVars = append(env.EnvVars, "XDG_CACHE_HOME="+cacheHome)

	// The shim is what makes `command git hop ...` -- the exact call both
	// the wrapper and the chdir handler emit -- land on the binary under
	// test. Everything else forwards to the system git, because the
	// wrapper also runs `git rev-parse` and the binary shells out to git
	// constantly.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found on PATH: %v", err)
	}
	shim := "#!/bin/sh\n" +
		"if [ \"$1\" = \"hop\" ]; then shift; exec " + strconv.Quote(env.BinPath) + " \"$@\"; fi\n" +
		"exec " + strconv.Quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(shim), 0755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}

	// The shim must come first on PATH for the shells, and only for them:
	// the harness's own RunGitHop calls the binary directly.
	shellEnv := make([]string, 0, len(env.EnvVars))
	for _, kv := range env.EnvVars {
		if strings.HasPrefix(kv, "PATH=") {
			kv = "PATH=" + binDir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
		}
		shellEnv = append(shellEnv, kv)
	}

	ce := &chainEnv{
		TestEnv:    env,
		CacheHome:  cacheHome,
		MarkerDir:  markerDir,
		BinDir:     binDir,
		shellEnv:   shellEnv,
		MainTree:   filepath.Join(env.HubPath, "hops", "main"),
		scriptRoot: filepath.Join(env.RootDir, "scripts"),
	}
	if err := os.MkdirAll(ce.scriptRoot, 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	ce.HooksDir = filepath.Join(env.HubPath, ".git-hop", "hooks")
	if err := os.MkdirAll(ce.HooksDir, 0755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}

	return ce
}

// integrationBlock returns the generated shell integration for shellType,
// written to a file the probe scripts source. Generated by a child of this
// test binary carrying this test's environment, so the cache path baked
// into the block points inside the test tree.
func (ce *chainEnv) integrationBlock(t *testing.T, shellType string) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}

	cmd := exec.Command(self, "-test.run="+genTestName)
	cmd.Env = append(append([]string{}, ce.shellEnv...), genEnvVar+"="+shellType)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("generate %s integration: %v", shellType, err)
	}
	if len(out) == 0 {
		t.Fatalf("generated %s integration is empty", shellType)
	}

	path := filepath.Join(ce.scriptRoot, "integration."+shellType)
	WriteFile(t, path, string(out))
	return path
}

// writeChainHook installs a post-worktree-switch hook that records the
// branch and trigger of every invocation and then exits with exitCode --
// or, when chdirOnly is set, exits that code only for the chdir trigger and
// 0 for the hop path, which is how a real navigating integration behaves
// when only one of the two paths needs the directive.
func (ce *chainEnv) writeChainHook(t *testing.T, exitCode int, chdirOnly bool) {
	t.Helper()

	body := "#!/bin/bash\n" +
		"mkdir -p \"$GIT_HOP_TEST_MARKER_DIR\"\n" +
		"printf '%s %s %s\\n' \"$GIT_HOP_BRANCH\" \"$GIT_HOP_TRIGGER\" \"$GIT_HOP_FROM_BRANCH\" >> \"$GIT_HOP_TEST_MARKER_DIR/chain.log\"\n"
	if chdirOnly {
		body += "if [ \"$GIT_HOP_TRIGGER\" = \"chdir\" ]; then exit " + strconv.Itoa(exitCode) + "; fi\nexit 0\n"
	} else {
		body += "exit " + strconv.Itoa(exitCode) + "\n"
	}

	path := filepath.Join(ce.HooksDir, "post-worktree-switch")
	WriteFile(t, path, body)
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("chmod hook: %v", err)
	}
}

// hookRecords returns one "branch trigger fromBranch" line per hook run, in
// the order they fired, or nil when the hook never ran.
func (ce *chainEnv) hookRecords(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ce.MarkerDir, "chain.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read hook log: %v", err)
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

// runProbe writes body as a script for shellType, sources the integration
// ahead of it, runs it from startDir, and returns the shell's final $PWD.
//
// The last line of output is the $PWD the script printed. Startup files are
// suppressed so a developer's own rc cannot put the machine's real git
// ahead of the shim and send the probe at the wrong binary.
//
// bash needs one extra thing: it has no chpwd hook, so the handler runs off
// PROMPT_COMMAND, which a non-interactive shell never fires. The probe
// bodies invoke it explicitly after each cd -- exactly what an interactive
// bash does at each prompt.
func (ce *chainEnv) runProbe(t *testing.T, shellBin, shellType, integrationPath, body, startDir string) (pwd string, output string) {
	t.Helper()

	// `source` is spelled the same in all three interpreters.
	var b strings.Builder
	b.WriteString("source " + strconv.Quote(integrationPath) + "\n")
	b.WriteString(body)
	b.WriteString("\nprintf 'HOPPWD=%s\\n' \"$PWD\"\n")

	scriptPath := filepath.Join(ce.scriptRoot, "probe."+shellType)
	WriteFile(t, scriptPath, b.String())

	var args []string
	switch shellType {
	case "zsh":
		args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
	case "bash":
		args = []string{"--noprofile", "--norc", scriptPath}
	case "fish":
		args = []string{"--no-config", scriptPath}
	default:
		t.Fatalf("unknown shell type %q", shellType)
	}

	cmd := exec.Command(shellBin, args...)
	cmd.Dir = startDir
	cmd.Env = ce.shellEnv
	raw, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("run %s: %v (output: %s)", shellBin, err, raw)
		}
	}
	output = string(raw)

	for _, line := range strings.Split(output, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "HOPPWD="); ok {
			pwd = v
		}
	}
	if pwd == "" {
		t.Fatalf("%s probe printed no PWD; output:\n%s", shellType, output)
	}
	if resolved, err := filepath.EvalSymlinks(pwd); err == nil {
		pwd = resolved
	}
	return pwd, output
}

// promptTick is the bash-only line that drives PROMPT_COMMAND after a cd.
func promptTick(shellType string) string {
	if shellType == "bash" {
		return "eval \"$PROMPT_COMMAND\"\n"
	}
	return ""
}

// TestChdirChain_WrapperHopPathMovesTheShell composes the generated wrapper
// with the real binary on the hop path, and asserts the thing the wrapper
// exists to do: the user's shell ends up in the branch they hopped to.
//
// It used to assert only what the wrapper OBSERVED -- that the switch landed
// and the status was 0 -- and said so explicitly, because the shell could
// not move. The cd resolved its target as `$(git rev-parse
// --show-toplevel)/../current` with `.../current` as a fallback, and in the
// real `hub/hops/<branch>` layout the toplevel is the worktree, so both
// candidates pointed below the hub and neither existed. Now that the target
// comes from the binary, the destination is assertable and asserted.
//
// Everything the wrapper reached before is still checked here: the `current`
// symlink really moved, through the real binary, which is what makes the
// landing a switch rather than a bare cd.
func TestChdirChain_WrapperHopPathMovesTheShell(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.RunGitHop(t, ce.HubPath, "add", "feature-a")
			integration := ce.integrationBlock(t, sh.shellType)

			body := "git-hop feature-a\n" +
				statusEcho(sh.shellType, "WRAPPER_STATUS")

			pwd, out := ce.runProbe(t, bin, sh.shellType, integration,
				body, ce.MainTree)

			if !strings.Contains(out, "WRAPPER_STATUS=0") {
				t.Errorf("%s: wrapper reported non-zero for a successful hop; output:\n%s",
					sh.shellType, out)
			}

			// The switch itself must have landed, through the real binary.
			target, err := os.Readlink(filepath.Join(ce.HubPath, "current"))
			if err != nil {
				t.Fatalf("current symlink unreadable: %v", err)
			}
			if want := filepath.Join("hops", "feature-a"); target != want {
				t.Errorf("%s: current = %q, want %q -- the wrapper never reached the real binary",
					sh.shellType, target, want)
			}

			// And the shell must have followed it. Starting in main and
			// ending in main is the exact shape of the old failure, so the
			// destination being a DIFFERENT worktree from the origin is what
			// makes this assertion mean anything.
			wantPwd := filepath.Join(ce.HubPath, "hops", "feature-a")
			if resolved, err := filepath.EvalSymlinks(wantPwd); err == nil {
				wantPwd = resolved
			}
			if pwd != wantPwd {
				t.Errorf("%s: shell ended at %q, want the branch it hopped to %q; output:\n%s",
					sh.shellType, pwd, wantPwd, out)
			}
		})
	}
}

// TestChdirChain_WrapperCdTargetResolvesInHopsLayout is the composed
// regression for the resolution itself, at the depths that broke it.
//
// It replaces a test that pinned the gap from the outside by asserting the
// two candidate paths did NOT exist. That test was correct about the old
// wrapper and is meaningless against the new one, which never forms those
// paths; what is worth pinning now is the property they were standing in
// for -- that the shell lands in the hub's `current` worktree from anywhere
// inside the hub.
//
// Two starting points, because the old code failed differently at each. From
// inside a worktree, `rev-parse --show-toplevel` answered with the worktree
// and the `..` count needed to reach the hub varied with how deep the branch
// name nested. From the bare hub there was no toplevel at all: rev-parse
// exited 128, the hub path came back empty, and the cd was skipped in
// silence. A nested branch name is used throughout so a fix that merely
// counted `..` correctly for single-segment branches cannot pass.
func TestChdirChain_WrapperCdTargetResolvesInHopsLayout(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.RunGitHop(t, ce.HubPath, "add", "feat/deep/name")
			integration := ce.integrationBlock(t, sh.shellType)

			deep := filepath.Join(ce.HubPath, "hops", "feat", "deep", "name")
			if resolved, err := filepath.EvalSymlinks(deep); err == nil {
				deep = resolved
			}

			for _, start := range []struct {
				name string
				dir  string
			}{
				{"inside a worktree", ce.MainTree},
				{"the bare hub", ce.HubPath},
			} {
				t.Run(start.name, func(t *testing.T) {
					body := "git-hop feat/deep/name\n"
					pwd, out := ce.runProbe(t, bin, sh.shellType, integration,
						body, start.dir)

					if pwd != deep {
						t.Errorf("%s: started in %s, shell ended at %q, want %q; output:\n%s",
							sh.shellType, start.name, pwd, deep, out)
					}
				})
			}
		})
	}
}

// TestChdirChain_HopPathDirectiveKeepsShellPut is the hop-path half of the
// directive contract, composed end to end.
//
// A post-worktree-switch hook that navigates the user itself exits the
// reserved code. The binary re-raises it as its process status, the wrapper
// reads that status, and the contract is that the wrapper reports success
// to the user while leaving the shell exactly where it was -- cd'ing on top
// of the hook is precisely what would put two windows in one worktree.
func TestChdirChain_HopPathDirectiveKeepsShellPut(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.RunGitHop(t, ce.HubPath, "add", "feature-a")
			ce.writeChainHook(t, hooks.ExitNavigationHandled, false)
			integration := ce.integrationBlock(t, sh.shellType)

			body := "git-hop feature-a\n" +
				statusEcho(sh.shellType, "WRAPPER_STATUS")

			pwd, out := ce.runProbe(t, bin, sh.shellType, integration,
				body, ce.MainTree)

			if pwd != ce.MainTree {
				t.Errorf("%s: shell moved to %q under the handled-navigation directive; it must stay at %q",
					sh.shellType, pwd, ce.MainTree)
			}

			// The directive means the command SUCCEEDED and navigation was
			// taken care of, so the user's shell must see 0 -- not 93,
			// which would surface as a failed command.
			if !strings.Contains(out, "WRAPPER_STATUS=0") {
				t.Errorf("%s: wrapper leaked the reserved code to the caller; output:\n%s",
					sh.shellType, out)
			}

			records := ce.hookRecords(t)
			if len(records) != 1 {
				t.Fatalf("%s: hook ran %d time(s), want 1: %v", sh.shellType, len(records), records)
			}
			if !strings.HasPrefix(records[0], "feature-a hop") {
				t.Errorf("%s: hook record = %q, want the hop trigger for feature-a", sh.shellType, records[0])
			}
		})
	}
}

// TestChdirChain_PlainCdFiresHookThroughRealBinary is the chdir path with
// nothing stubbed.
//
// A plain `cd` into a registered worktree never touches the binary on its
// own. The generated handler is what turns it into an event: it string-tests
// $PWD against the roots cache the REAL binary wrote, and on a hit forks the
// REAL binary's hidden notify subcommand, which re-verifies against hop.json
// before firing post-worktree-switch.
//
// GIT_HOP_TRIGGER is the assertion that this went down the chdir path rather
// than the hop path: both fire the same hook, and only the trigger tells a
// hook which one it is looking at.
func TestChdirChain_PlainCdFiresHookThroughRealBinary(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.RunGitHop(t, ce.HubPath, "add", "feature-a")
			ce.RunGitHop(t, ce.HubPath, "add", "feature-b")
			ce.writeChainHook(t, 0, false)
			integration := ce.integrationBlock(t, sh.shellType)

			treeA := filepath.Join(ce.HubPath, "hops", "feature-a")
			treeB := filepath.Join(ce.HubPath, "hops", "feature-b")

			tick := promptTick(sh.shellType)
			body := "cd " + strconv.Quote(treeA) + "\n" + tick +
				"cd " + strconv.Quote(treeB) + "\n" + tick

			pwd, out := ce.runProbe(t, bin, sh.shellType, integration, body, ce.RootDir)

			// No directive, so the user's cd stands exactly as typed.
			if pwd != treeB {
				t.Errorf("%s: shell ended at %q, want the destination %q; output:\n%s",
					sh.shellType, pwd, treeB, out)
			}

			records := ce.hookRecords(t)
			if len(records) != 2 {
				t.Fatalf("%s: hook ran %d time(s), want 2 (one per worktree entered): %v",
					sh.shellType, len(records), records)
			}
			// First arrival has no registered origin; the second names it.
			if records[0] != "feature-a chdir" {
				t.Errorf("%s: first record = %q, want %q", sh.shellType, records[0], "feature-a chdir")
			}
			if records[1] != "feature-b chdir feature-a" {
				t.Errorf("%s: second record = %q, want %q",
					sh.shellType, records[1], "feature-b chdir feature-a")
			}
		})
	}
}

// TestChdirChain_ChdirDirectiveRestoresOrigin is the composed version of
// the restore regression.
//
// internal/shell proves the handler restores when a STUB git exits the
// reserved code. That leaves the interesting half unproven: that a real
// hook, run by the real hook runner, inside the real notify subcommand,
// actually produces that status at the shell. Every link is real here --
// the only thing the test supplies is a hook that navigates.
//
// The failure this pins: the user cd's A -> B, a hook selects B's window
// elsewhere, and the shell that ran the cd is left in B as well. Two places
// pointing into one worktree, with A's pane no longer in A.
func TestChdirChain_ChdirDirectiveRestoresOrigin(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.RunGitHop(t, ce.HubPath, "add", "feature-a")
			ce.RunGitHop(t, ce.HubPath, "add", "feature-b")
			// Directive on the chdir path only: this is the path where the
			// user has ALREADY moved and the shell has to be put back.
			ce.writeChainHook(t, hooks.ExitNavigationHandled, true)
			integration := ce.integrationBlock(t, sh.shellType)

			treeA := filepath.Join(ce.HubPath, "hops", "feature-a")
			treeB := filepath.Join(ce.HubPath, "hops", "feature-b")

			tick := promptTick(sh.shellType)
			body := "cd " + strconv.Quote(treeA) + "\n" + tick +
				"cd " + strconv.Quote(treeB) + "\n" + tick

			pwd, out := ce.runProbe(t, bin, sh.shellType, integration, body, ce.RootDir)

			if pwd != treeA {
				t.Errorf("%s: shell left at %q; a hook that handled navigation must leave the originating shell restored to %q; output:\n%s",
					sh.shellType, pwd, treeA, out)
			}

			// Exactly two hook runs. A restore performed outside the
			// re-entrancy guard would cd back into A, re-enter the handler,
			// notify again, and ping-pong; the count is what catches it.
			records := ce.hookRecords(t)
			if len(records) != 2 {
				t.Errorf("%s: hook ran %d time(s), want exactly 2 -- more means the restore re-entered the handler: %v",
					sh.shellType, len(records), records)
			}
		})
	}
}

// TestChdirChain_WorktreeCreatedMidSessionIsDetected is the composed
// version of the mid-session detection regression.
//
// The handler tests $PWD against an array slurped from the roots cache when
// the shell STARTED. A worktree created afterwards is in the file on disk
// and absent from the array in memory, so plain-cd detection is blind to
// exactly the worktree the user just made -- the most likely one for them
// to cd into next. On a fresh install the array starts empty, which is why
// the feature could look like it did nothing at all.
//
// One shell session does all of it: source the integration, create the
// worktree through the wrapper, then plain-cd into it. Both halves of the
// fix have to hold for this to pass -- the binary must rewrite the cache on
// `add`, and the wrapper must re-slurp it after the binary returns.
func TestChdirChain_WorktreeCreatedMidSessionIsDetected(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.writeChainHook(t, 0, false)
			integration := ce.integrationBlock(t, sh.shellType)

			fresh := filepath.Join(ce.HubPath, "hops", "fresh-branch")

			tick := promptTick(sh.shellType)
			body := "git-hop add fresh-branch\n" +
				"cd " + strconv.Quote(fresh) + "\n" + tick

			pwd, out := ce.runProbe(t, bin, sh.shellType, integration, body, ce.MainTree)

			if pwd != fresh {
				t.Fatalf("%s: shell ended at %q, want the new worktree %q; output:\n%s",
					sh.shellType, pwd, fresh, out)
			}

			// The hook firing for fresh-branch on the chdir trigger is the
			// assertion: it can only happen if the live shell's in-memory
			// roots array learned about a worktree created after it started.
			records := ce.hookRecords(t)
			var sawFreshChdir bool
			for _, r := range records {
				if strings.HasPrefix(r, "fresh-branch chdir") {
					sawFreshChdir = true
				}
			}
			if !sawFreshChdir {
				t.Errorf("%s: plain cd into a worktree created this session did not reach the binary; hook records: %v; output:\n%s",
					sh.shellType, records, out)
			}
		})
	}
}

// statusEcho prints the previous command's exit status under a label, in
// the shell's own spelling.
func statusEcho(shellType, label string) string {
	if shellType == "fish" {
		return "printf '" + label + "=%s\\n' $status\n"
	}
	return "printf '" + label + "=%s\\n' \"$?\"\n"
}
