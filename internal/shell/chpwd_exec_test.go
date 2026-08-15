package shell_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/shell"
)

// The chdir handler is shell source that runs on every prompt, so string
// assertions on it prove nothing about what the shell actually does with
// it -- the previous change in this area shipped a dead `case` arm and a
// fish bracket class that fish cannot evaluate, both invisible to text
// tests and both caught only by running the real interpreters.
//
// These tests execute the generated handler in bash, zsh and fish with a
// stub `git` that RECORDS EVERY INVOCATION to a log file. That log is the
// instrument: "spawns no subprocess" is not asserted by reading the source,
// it is asserted by the log being empty afterwards.

// chdirFixture is a set of registered/unregistered directories on the real
// filesystem, plus a roots cache and a fork-recording stub git.
type chdirFixture struct {
	root      string
	worktreeA string // registered
	worktreeB string // registered
	subA      string // subdirectory of worktreeA -- same worktree
	unrelated string // NOT registered
	binDir    string
	forkLog   string
}

// newChdirFixture lays out the directories and writes the roots cache the
// generated handler reads. The cache path is fixed at generation time
// (it embeds hop.GetGitHopCacheHome()), so the cache is redirected by
// pointing XDG_CACHE_HOME at the fixture before generating the handler.
func newChdirFixture(t *testing.T) chdirFixture {
	t.Helper()

	// Resolve the temp root before deriving any path from it. On macOS
	// t.TempDir() hands back /var/... while /var is a symlink to
	// /private/var, so a shell started in that directory inherits the
	// RESOLVED $PWD but reports the unresolved one after an explicit cd.
	// Two spellings of one directory would make the fixture's own paths
	// disagree with $PWD and manufacture failures that say nothing about
	// the handler.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}

	fx := chdirFixture{
		root:      root,
		worktreeA: filepath.Join(root, "hub", "hops", "feat", "alpha"),
		worktreeB: filepath.Join(root, "hub", "hops", "beta"),
		unrelated: filepath.Join(root, "elsewhere", "some", "project"),
		binDir:    filepath.Join(root, "bin"),
		forkLog:   filepath.Join(root, "forks.log"),
	}
	fx.subA = filepath.Join(fx.worktreeA, "internal", "pkg")

	for _, d := range []string{fx.worktreeA, fx.worktreeB, fx.subA, fx.unrelated, fx.binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Stub git records every invocation. Anything the handler forks shows
	// up here -- including forks that are not `git hop`, because the stub
	// logs argv unconditionally.
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + fx.forkLog + "\"\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(fx.binDir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub git: %v", err)
	}
	if err := os.WriteFile(fx.forkLog, nil, 0o644); err != nil {
		t.Fatalf("create fork log: %v", err)
	}

	return fx
}

// writeRootsCache writes the roots cache at the path the generated handler
// was compiled to read.
func writeRootsCache(t *testing.T, cachePath string, roots ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	body := strings.Join(roots, "\n") + "\n"
	if err := os.WriteFile(cachePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write roots cache: %v", err)
	}
}

// generateWithCacheHome regenerates the handler with XDG_CACHE_HOME pointed
// at dir, so the emitted absolute cache path lands inside the fixture
// instead of the developer's real cache.
func generateWithCacheHome(t *testing.T, shellType, cacheHome string) (src, cachePath string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	return shell.GenerateChdirHandler(shellType), shell.RootsCachePath()
}

// runChdirScript sources the handler and performs a sequence of cds,
// returning everything the stub git recorded.
//
// The cds go through the shell's own `cd` so the real hook mechanism runs:
// zsh's chpwd, fish's PWD variable event, and bash's PROMPT_COMMAND (which
// a non-interactive shell never fires on its own, so it is invoked
// explicitly after each cd -- that is exactly what an interactive bash
// does at each prompt).
func runChdirScript(t *testing.T, shellBin, shellType, handlerSrc string, fx chdirFixture, cds []string, startDir string) []string {
	t.Helper()

	var b strings.Builder
	b.WriteString(handlerSrc)
	b.WriteString("\n")

	for _, d := range cds {
		switch shellType {
		case "fish":
			b.WriteString("cd '" + d + "'\n")
		case "zsh":
			b.WriteString("cd '" + d + "'\n")
		default:
			// bash: PROMPT_COMMAND is what an interactive shell runs at
			// each prompt. Drive it the same way here.
			b.WriteString("cd '" + d + "'\n")
			b.WriteString("eval \"$PROMPT_COMMAND\"\n")
		}
	}

	scriptPath := filepath.Join(t.TempDir(), "probe."+shellType)
	if err := os.WriteFile(scriptPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	// Start-up files must be suppressed: a real user's rc rewrites PATH,
	// which puts the machine's actual git ahead of the recording stub and
	// makes the probe measure the wrong binary entirely.
	var args []string
	switch shellType {
	case "zsh":
		args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
	case "bash":
		args = []string{"--noprofile", "--norc", scriptPath}
	case "fish":
		args = []string{"--no-config", scriptPath}
	}

	cmd := exec.Command(shellBin, args...)
	cmd.Dir = startDir
	cmd.Env = append(os.Environ(), "PATH="+fx.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("run %s: %v (output: %s)", shellBin, err, out)
		}
	}

	return readForkLog(t, fx.forkLog)
}

// readForkLog returns every command the stub git recorded.
func readForkLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fork log: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

var chdirShells = []struct{ bin, shellType string }{
	{"bash", "bash"},
	{"zsh", "zsh"},
	{"fish", "fish"},
}

// TestChdirExec_UnrelatedDirectorySpawnsNothing is the load-bearing test of
// this whole change.
//
// The handler runs on every prompt, and almost every cd a user makes is NOT
// into a worktree. If that path forks, it taxes every command they run.
// "No fork" is asserted directly: the stub git on PATH appends to a log on
// every invocation, and after cd'ing somewhere unregistered the log must be
// byte-for-byte empty.
func TestChdirExec_UnrelatedDirectorySpawnsNothing(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.unrelated}, fx.root)

			if len(forks) != 0 {
				t.Errorf("%s: cd into unregistered dir spawned %d subprocess(es), want 0: %v",
					sh.shellType, len(forks), forks)
			}
		})
	}
}

// TestChdirExec_RegisteredWorktreeNotifies pins the positive case: a cd
// into a registered worktree must reach the hidden notify subcommand.
func TestChdirExec_RegisteredWorktreeNotifies(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA}, fx.root)

			if len(forks) != 1 {
				t.Fatalf("%s: cd into registered worktree produced %d invocation(s), want 1: %v",
					sh.shellType, len(forks), forks)
			}
			want := "hop " + shell.NotifyChdirCommand + " " + fx.worktreeA
			if forks[0] != want {
				t.Errorf("%s: invoked %q, want %q", sh.shellType, forks[0], want)
			}
		})
	}
}

// TestChdirExec_SameWorktreeDoesNotRefire covers movement that is not a
// switch: into a worktree, then deeper into it, then back to its root.
// Only the first transition is a switch; the rest are the user moving
// around inside one worktree and must stay silent.
func TestChdirExec_SameWorktreeDoesNotRefire(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.subA, fx.worktreeA}, fx.root)

			if len(forks) != 1 {
				t.Errorf("%s: moving within one worktree produced %d invocation(s), want exactly 1: %v",
					sh.shellType, len(forks), forks)
			}
		})
	}
}

// TestChdirExec_NoOpCdDoesNotFire pins that cd'ing to where you already are
// is not a switch. This matters most for bash, whose PROMPT_COMMAND fires
// on every prompt regardless of whether anything moved.
func TestChdirExec_NoOpCdDoesNotFire(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			// Start already inside worktreeA, then cd to it repeatedly.
			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeA, fx.worktreeA}, fx.worktreeA)

			if len(forks) != 0 {
				t.Errorf("%s: no-op cd inside the starting worktree produced %d invocation(s), want 0: %v",
					sh.shellType, len(forks), forks)
			}
		})
	}
}

// TestChdirExec_WorktreeToWorktreeFires pins that moving between two
// distinct registered worktrees reports each arrival -- this is the desync
// the change exists to prevent.
func TestChdirExec_WorktreeToWorktreeFires(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeB}, fx.root)

			if len(forks) != 2 {
				t.Fatalf("%s: A->B produced %d invocation(s), want 2: %v",
					sh.shellType, len(forks), forks)
			}
			if !strings.HasSuffix(forks[0], fx.worktreeA) {
				t.Errorf("%s: first notify was %q, want it to name %s", sh.shellType, forks[0], fx.worktreeA)
			}
			if !strings.HasSuffix(forks[1], fx.worktreeB) {
				t.Errorf("%s: second notify was %q, want it to name %s", sh.shellType, forks[1], fx.worktreeB)
			}
		})
	}
}

// TestChdirExec_PassesFromWorktreeExplicitly pins how from-state reaches
// the binary on the chdir path.
//
// It must be passed in the environment of the notify call rather than left
// to $OLDPWD. zsh reports OLDPWD as exported, but it arrives EMPTY in the
// subprocess -- so reading it there silently emptied GIT_HOP_FROM_BRANCH on
// every chdir switch. Only running the real shells surfaced that; the Go
// tests were passing the value in directly and saw nothing wrong.
//
// The stub git records its own environment, so this asserts what the child
// actually received.
func TestChdirExec_PassesFromWorktreeExplicitly(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			// Re-stub git to record the from-variable as the child sees it.
			stub := "#!/bin/sh\n" +
				"printf 'from=[%s]\\n' \"$GIT_HOP_CHDIR_FROM\" >> \"" + fx.forkLog + "\"\n" +
				"exit 0\n"
			if err := os.WriteFile(filepath.Join(fx.binDir, "git"), []byte(stub), 0o755); err != nil {
				t.Fatalf("write stub git: %v", err)
			}

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.worktreeB}, fx.root)

			if len(forks) != 2 {
				t.Fatalf("%s: expected 2 notifies, got %d: %v", sh.shellType, len(forks), forks)
			}
			// First arrival has no origin worktree.
			if forks[0] != "from=[]" {
				t.Errorf("%s: first notify carried %q, want an empty from", sh.shellType, forks[0])
			}
			// A->B must name A, in the CHILD's environment.
			if want := "from=[" + fx.worktreeA + "]"; forks[1] != want {
				t.Errorf("%s: second notify carried %q, want %q", sh.shellType, forks[1], want)
			}
		})
	}
}

// TestChdirExec_SiblingPrefixIsNotAMatch guards the classic prefix bug: a
// directory whose path merely STARTS WITH a worktree path is not inside it.
// Without the separator in the comparison, "/w/alpha-scratch" reads as
// being under "/w/alpha".
func TestChdirExec_SiblingPrefixIsNotAMatch(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			sibling := fx.worktreeA + "-scratch"
			if err := os.MkdirAll(sibling, 0o755); err != nil {
				t.Fatalf("mkdir sibling: %v", err)
			}

			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{sibling}, fx.root)

			if len(forks) != 0 {
				t.Errorf("%s: cd into sibling %q produced %d invocation(s), want 0: %v",
					sh.shellType, sibling, len(forks), forks)
			}
		})
	}
}

// TestChdirExec_ReentrancyGuarded covers a hook that itself cds.
//
// post-worktree-switch hooks are expected to move the user around -- that
// is much of what they are for -- and a hook that cds while the handler is
// still on the stack re-enters it.
//
// Only zsh turns out to be exposed: chpwd fires synchronously on that cd,
// so with the guard removed this goes red for zsh and zsh alone (2 notifies
// instead of 1). bash reaches the handler through PROMPT_COMMAND, which a
// mid-flight cd does not re-trigger, and fish does not nest --on-variable
// events during handler execution. The guard is emitted for all three
// regardless: it costs one string test on a path that has already decided
// to fork, and betting on two interpreters' non-nesting behaviour staying
// that way is worse value than setting a variable.
func TestChdirExec_ReentrancyGuarded(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			// The cd has to happen WHILE the handler is on the stack --
			// that is what re-entrancy means, and it is the only thing the
			// guard can affect. A cd issued after the handler returned is
			// just another switch, and counting those measures nothing.
			//
			// Provoking it takes some care. The handler reaches the binary
			// through `command git`, which deliberately bypasses shell
			// functions, so overriding git() is never called and the test
			// would pass vacuously no matter what the guard did. Overriding
			// `command` itself is what lands the cd inside the handler's
			// own execution window, standing in for a hook that navigates.
			var b strings.Builder
			b.WriteString(src)
			b.WriteString("\n")
			switch sh.shellType {
			case "fish":
				b.WriteString("function command\n" +
					"  builtin command $argv\n" +
					"  builtin cd '" + fx.worktreeB + "'\n" +
					"end\n")
				b.WriteString("cd '" + fx.worktreeA + "'\n")
			default:
				b.WriteString("command() {\n" +
					"  builtin command \"$@\"\n" +
					"  builtin cd '" + fx.worktreeB + "'\n" +
					"}\n")
				b.WriteString("cd '" + fx.worktreeA + "'\n")
				if sh.shellType == "bash" {
					b.WriteString("eval \"$PROMPT_COMMAND\"\n")
				}
			}

			scriptPath := filepath.Join(t.TempDir(), "probe."+sh.shellType)
			if err := os.WriteFile(scriptPath, []byte(b.String()), 0o644); err != nil {
				t.Fatalf("write probe: %v", err)
			}

			var args []string
			switch sh.shellType {
			case "zsh":
				args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
			case "bash":
				args = []string{"--noprofile", "--norc", scriptPath}
			case "fish":
				args = []string{"--no-config", scriptPath}
			}

			cmd := exec.Command(bin, args...)
			cmd.Dir = fx.root
			cmd.Env = append(os.Environ(), "PATH="+fx.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, _ := cmd.CombinedOutput()

			forks := readForkLog(t, fx.forkLog)

			// Exactly one notify: the arrival the user caused. The cd the
			// "hook" performs mid-flight must be absorbed by the guard.
			// Without the guard the handler re-enters and notifies again.
			if len(forks) != 1 {
				t.Errorf("%s: hook cd'ing mid-handler produced %d notify(s), want exactly 1: %v (output: %s)",
					sh.shellType, len(forks), forks, out)
			}
		})
	}
}

// TestChdirExec_DisableEnvVarSilencesHandler exercises the escape hatch.
//
// The handler is unconditional within an installed wrapper, so the opt-out
// is the only way to switch it off without editing an rc file -- which is
// what someone bisecting a slow prompt needs. An untested opt-out is worse
// than none, because it fails exactly when a user has already concluded
// git-hop is at fault.
func TestChdirExec_DisableEnvVarSilencesHandler(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, cachePath := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			writeRootsCache(t, cachePath, fx.worktreeA, fx.worktreeB)

			var b strings.Builder
			b.WriteString(src)
			b.WriteString("\n")
			// Set the opt-out, then make a cd that would otherwise notify.
			if sh.shellType == "fish" {
				b.WriteString("set -gx GIT_HOP_NO_CHDIR_HOOK 1\n")
			} else {
				b.WriteString("export GIT_HOP_NO_CHDIR_HOOK=1\n")
			}
			b.WriteString("cd '" + fx.worktreeA + "'\n")
			if sh.shellType == "bash" {
				b.WriteString("eval \"$PROMPT_COMMAND\"\n")
			}

			scriptPath := filepath.Join(t.TempDir(), "probe."+sh.shellType)
			if err := os.WriteFile(scriptPath, []byte(b.String()), 0o644); err != nil {
				t.Fatalf("write probe: %v", err)
			}

			var args []string
			switch sh.shellType {
			case "zsh":
				args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
			case "bash":
				args = []string{"--noprofile", "--norc", scriptPath}
			case "fish":
				args = []string{"--no-config", scriptPath}
			}

			cmd := exec.Command(bin, args...)
			cmd.Dir = fx.root
			cmd.Env = append(os.Environ(), "PATH="+fx.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, _ := cmd.CombinedOutput()

			if forks := readForkLog(t, fx.forkLog); len(forks) != 0 {
				t.Errorf("%s: opt-out set but handler still spawned %d subprocess(es): %v (output: %s)",
					sh.shellType, len(forks), forks, out)
			}
		})
	}
}

// TestChdirExec_EmptyCacheSpawnsNothing covers the pre-first-hop state: no
// cache file at all. The handler must stay inert rather than fall back to
// asking the binary on every prompt.
func TestChdirExec_EmptyCacheSpawnsNothing(t *testing.T) {
	for _, sh := range chdirShells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newChdirFixture(t)
			src, _ := generateWithCacheHome(t, sh.shellType, filepath.Join(fx.root, "cache"))
			// Deliberately no cache written.

			forks := runChdirScript(t, bin, sh.shellType, src, fx,
				[]string{fx.worktreeA, fx.unrelated, fx.worktreeB}, fx.root)

			if len(forks) != 0 {
				t.Errorf("%s: absent cache produced %d invocation(s), want 0: %v",
					sh.shellType, len(forks), forks)
			}
		})
	}
}
