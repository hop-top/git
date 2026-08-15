package shell_test

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

// The generated wrapper is shell source, so string assertions on it prove
// only that text exists -- not that the shell does the right thing with
// it. These tests source the real block in the real interpreter with a
// stub `git` on PATH and assert the shell's resulting cwd and status.
//
// The three cases that matter:
//
//	exit 0  -> cd into the hub's `current` worktree
//	exit 93 -> do NOT cd, and report 0 (the hook already navigated)
//	exit 1  -> do NOT cd, and propagate 1

// wrapperFixture is a hub laid out the way git-hop actually lays one out,
// on the real filesystem so a real shell can walk it.
//
// The layout matters more than it looks. The earlier version of this
// fixture put `current` next to a worktree that lived OUTSIDE any `hops/`
// directory, and stubbed `git rev-parse --show-toplevel` to answer with the
// hub. Both departures from reality were load-bearing: in a real hub the
// worktree is `<hub>/hops/<branch>` and `rev-parse` run from inside it
// answers with the WORKTREE, never the hub. A wrapper that resolves its cd
// target relative to the toplevel therefore looks correct against the old
// fixture and finds nothing at all against a user's machine.
//
// So: real `hops/<branch>` nesting, `current` at the hub root, and no
// stubbed `rev-parse` -- the stub git forwards everything except `hop` to
// the system git, which runs against a real repository.
type wrapperFixture struct {
	hub      string // hub root: holds hop.json and the `current` symlink
	dir      string // where the shell starts
	worktree string // what `current` points at -- the expected cd target
	binDir   string // holds the stub git
}

// wrapperFixtureOpts selects which of the real layout's shapes to build.
type wrapperFixtureOpts struct {
	// hopExit is the status the stub git returns for `git hop ...`.
	hopExit int

	// branch is the worktree `current` points at, as a path under `hops/`.
	// Branch names nest arbitrarily ("main", "fix/a", "a/b/c"), and the
	// depth is exactly what a `..`-based resolution gets wrong, so it is a
	// parameter rather than a constant.
	branch string

	// startAtHub starts the shell in the bare hub itself rather than in the
	// origin worktree. There `git rev-parse --show-toplevel` fails
	// outright, so any resolution keyed on it has nothing to key on.
	startAtHub bool
}

// newWrapperFixture builds the hub described by opts.
func newWrapperFixture(t *testing.T, opts wrapperFixtureOpts) wrapperFixture {
	t.Helper()

	if opts.branch == "" {
		opts.branch = "feature"
	}

	root := t.TempDir()
	hub := filepath.Join(root, "hub")
	worktree := filepath.Join(hub, "hops", filepath.FromSlash(opts.branch))
	// The shell starts in a DIFFERENT worktree from the one `current`
	// names. Starting in the destination would make "never moved" and
	// "moved correctly" the same observation, and the bug is exactly a
	// wrapper that never moves.
	origin := filepath.Join(hub, "hops", "origin-tree")
	binDir := filepath.Join(root, "bin")

	for _, d := range []string{hub, worktree, origin, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// hop.json is what marks a directory as a hub -- the same marker the
	// binary's own hub discovery walks for.
	if err := os.WriteFile(filepath.Join(hub, "hop.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write hop.json: %v", err)
	}

	// The real symlink is relative, hub-rooted, exactly as
	// hop.UpdateCurrentSymlink writes it.
	rel, err := filepath.Rel(hub, worktree)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if err := os.Symlink(rel, filepath.Join(hub, "current")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Real git repositories inside the worktrees, so `rev-parse` answers
	// the way it answers on a user's machine: with the worktree, not the
	// hub.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not available on this machine: %v", err)
	}
	for _, wt := range []string{worktree, origin} {
		initRepo := exec.Command(realGit, "init", "-q", wt)
		initRepo.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := initRepo.CombinedOutput(); err != nil {
			t.Fatalf("git init %s: %v (%s)", wt, err, out)
		}
	}

	// Stub git. Three behaviours, in the order the wrapper asks for them:
	//
	//   git hop __current-path  -> walk up from $PWD for hop.json, then
	//                              print that hub's `current` target. This
	//                              is a shell transcription of what the
	//                              binary's hidden subcommand does, kept
	//                              here so this package's tests stay free of
	//                              a compiled binary; test/e2e composes the
	//                              real one.
	//   git hop <anything else> -> exit hopExit, the switch under test.
	//   everything else         -> the real git, untouched.
	stub := "#!/bin/sh\n" +
		"if [ \"$1\" = \"hop\" ] && [ \"$2\" = " + strconv.Quote(shell.CurrentPathCommand) + " ]; then\n" +
		"  d=$PWD\n" +
		"  while [ \"$d\" != \"/\" ]; do\n" +
		"    if [ -f \"$d/hop.json\" ]; then\n" +
		"      [ -d \"$d/current\" ] && (cd \"$d/current\" && pwd)\n" +
		"      exit 0\n" +
		"    fi\n" +
		"    d=$(dirname \"$d\")\n" +
		"  done\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"hop\" ]; then exit " + itoa(opts.hopExit) + "; fi\n" +
		"exec " + strconv.Quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub git: %v", err)
	}

	start := origin
	if opts.startAtHub {
		start = hub
	}

	return wrapperFixture{hub: hub, dir: start, worktree: worktree, binDir: binDir}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for n := i; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}
	return digits
}

// runWrapper sources the generated wrapper in `shellBin`, invokes
// `git-hop` with the given argument, and reports the shell's final cwd and
// the wrapper's exit status.
func runWrapper(t *testing.T, shellBin, shellType string, fx wrapperFixture, arg string) (cwd string, status int) {
	t.Helper()

	src := shell.GenerateWrapperFunction(shellType)
	scriptPath := filepath.Join(t.TempDir(), "probe."+shellType)

	invocation := "git-hop " + arg
	if arg == "" {
		invocation = "git-hop"
	}

	var script string
	if shellType == "fish" {
		// fish: capture the wrapper's status, print cwd, exit with it.
		script = src + "\n" + invocation + "\nset -l rc $status\npwd\nexit $rc\n"
	} else {
		script = src + "\n" + invocation + "\nrc=$?\npwd\nexit $rc\n"
	}

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	// Start-up files must be suppressed: a real user's zshenv/bashrc
	// rewrites PATH, which puts the machine's actual git ahead of the
	// stub and makes the probe exercise the wrong binary entirely.
	var args []string
	switch shellType {
	case "zsh":
		args = []string{"--no-rcs", "--no-globalrcs", scriptPath}
	case "bash":
		args = []string{"--noprofile", "--norc", scriptPath}
	case "fish":
		args = []string{"--no-config", scriptPath}
	default:
		args = []string{scriptPath}
	}

	cmd := exec.Command(shellBin, args...)
	cmd.Dir = fx.dir
	// Stub git must win over the real one.
	cmd.Env = append(os.Environ(), "PATH="+fx.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			status = ee.ExitCode()
		} else {
			t.Fatalf("run %s: %v", shellBin, err)
		}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		t.Fatalf("%s produced no output", shellBin)
	}
	cwd = strings.TrimSpace(lines[len(lines)-1])

	// Resolve symlinks on both sides: macOS /var -> /private/var makes a
	// raw string compare meaningless.
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	return cwd, status
}

// lookShell finds an interpreter, skipping the test when it is absent
// rather than failing -- not every machine has fish.
func lookShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available on this machine: %v", name, err)
	}
	return path
}

func resolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

// TestWrapperExec_ExitCodeBehaviour executes the generated wrapper in every
// interpreter present and pins the cd/status contract for 0, 93 and 1.
func TestWrapperExec_ExitCodeBehaviour(t *testing.T) {
	shells := []struct {
		bin       string
		shellType string
	}{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	}

	cases := []struct {
		name       string
		hopExit    int
		wantStatus int
		wantCd     bool
	}{
		{
			name:       "exit 0 cds into current worktree",
			hopExit:    0,
			wantStatus: 0,
			wantCd:     true,
		},
		{
			// The whole point of this change: the hook already moved the
			// user, so the wrapper must not cd, and must not surface the
			// directive as a failure.
			name:       "navigation-handled directive does not cd and reports success",
			hopExit:    hooks.ExitNavigationHandled,
			wantStatus: 0,
			wantCd:     false,
		},
		{
			name:       "genuine failure does not cd and propagates status",
			hopExit:    1,
			wantStatus: 1,
			wantCd:     false,
		},
	}

	for _, sh := range shells {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					fx := newWrapperFixture(t, wrapperFixtureOpts{hopExit: tc.hopExit})
					cwd, status := runWrapper(t, bin, sh.shellType, fx, "somebranch")

					if status != tc.wantStatus {
						t.Errorf("%s wrapper: exit status = %d, want %d (git hop exited %d)",
							sh.shellType, status, tc.wantStatus, tc.hopExit)
					}

					wantDir := resolve(t, fx.dir)
					if tc.wantCd {
						wantDir = resolve(t, fx.worktree)
					}

					if cwd != wantDir {
						t.Errorf("%s wrapper: cwd = %q, want %q (git hop exited %d, wantCd=%v)",
							sh.shellType, cwd, wantDir, tc.hopExit, tc.wantCd)
					}
				})
			}
		})
	}
}

// TestWrapperExec_CdLandsInCurrentFromInsideAWorktree is the regression for
// the resolution the wrapper used to perform.
//
// It resolved its target as `$(git rev-parse --show-toplevel)/../current`,
// falling back to `.../current`. Run where users actually run it -- inside
// `<hub>/hops/<branch>` -- the toplevel is the WORKTREE, so the two
// candidates are `<hub>/hops/current` and `<hub>/hops/<branch>/current`.
// The symlink git-hop maintains is `<hub>/current`. Both candidates miss,
// every time, and the shell never moved.
//
// Branch depth is swept because `..` arithmetic is only ever right by
// accident at one depth, and branch names nest arbitrarily. A single-segment
// branch is included precisely because a reader could otherwise dismiss the
// bug as a nesting artifact -- it misses there too.
func TestWrapperExec_CdLandsInCurrentFromInsideAWorktree(t *testing.T) {
	branches := []string{"main", "fix/chpwd", "a/b/c"}

	for _, sh := range []struct{ bin, shellType string }{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	} {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			for _, branch := range branches {
				t.Run(branch, func(t *testing.T) {
					fx := newWrapperFixture(t, wrapperFixtureOpts{branch: branch})
					cwd, status := runWrapper(t, bin, sh.shellType, fx, "somebranch")

					if status != 0 {
						t.Errorf("%s wrapper: exit status = %d, want 0", sh.shellType, status)
					}
					if want := resolve(t, fx.worktree); cwd != want {
						t.Errorf("%s wrapper: branch %q left the shell at %q, want the hub's current worktree %q",
							sh.shellType, branch, cwd, want)
					}
				})
			}
		})
	}
}

// TestWrapperExec_CdLandsInCurrentFromTheBareHub is the second failure mode
// of the same resolution.
//
// The bare hub holds git internals, not a work tree, so `git rev-parse
// --show-toplevel` run there exits 128 and prints nothing. The old wrapper
// read that empty string, found `hub_root` unset, and skipped the cd
// silently -- so `git hop <branch>` typed from the hub, the most natural
// place to type it, moved nothing.
func TestWrapperExec_CdLandsInCurrentFromTheBareHub(t *testing.T) {
	for _, sh := range []struct{ bin, shellType string }{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	} {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			fx := newWrapperFixture(t, wrapperFixtureOpts{branch: "fix/chpwd", startAtHub: true})
			cwd, status := runWrapper(t, bin, sh.shellType, fx, "somebranch")

			if status != 0 {
				t.Errorf("%s wrapper: exit status = %d, want 0", sh.shellType, status)
			}
			if want := resolve(t, fx.worktree); cwd != want {
				t.Errorf("%s wrapper: run from the hub, the shell ended at %q, want the current worktree %q",
					sh.shellType, cwd, want)
			}
		})
	}
}

// TestWrapperExec_CdDoesNotFireOutsideAHub is the other half of the
// contract: resolving the hub more aggressively must not start moving people
// who are not in a hub at all.
//
// A plain git repository with no hub anywhere above it is the case that
// keeps an upward walk honest. The wrapper must run the command and leave
// the shell exactly where it was.
func TestWrapperExec_CdDoesNotFireOutsideAHub(t *testing.T) {
	for _, sh := range []struct{ bin, shellType string }{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	} {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Skipf("git not available: %v", err)
			}

			root := t.TempDir()
			repo := filepath.Join(root, "unrelated")
			binDir := filepath.Join(root, "bin")
			for _, d := range []string{repo, binDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", d, err)
				}
			}

			initRepo := exec.Command(realGit, "init", "-q", repo)
			initRepo.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			if out, err := initRepo.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v (%s)", err, out)
			}

			// No hop.json anywhere above, so the current-path query has
			// nothing to find and answers with silence -- exactly what the
			// binary does outside a hub.
			stub := "#!/bin/sh\n" +
				"if [ \"$1\" = \"hop\" ] && [ \"$2\" = " + strconv.Quote(shell.CurrentPathCommand) + " ]; then exit 0; fi\n" +
				"if [ \"$1\" = \"hop\" ]; then exit 0; fi\n" +
				"exec " + strconv.Quote(realGit) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(stub), 0o755); err != nil {
				t.Fatalf("write stub git: %v", err)
			}

			fx := wrapperFixture{hub: repo, dir: repo, worktree: repo, binDir: binDir}
			cwd, status := runWrapper(t, bin, sh.shellType, fx, "somebranch")

			if status != 0 {
				t.Errorf("%s wrapper: exit status = %d, want 0", sh.shellType, status)
			}
			if want := resolve(t, repo); cwd != want {
				t.Errorf("%s wrapper: moved the shell to %q from an unrelated repo; it must stay at %q",
					sh.shellType, cwd, want)
			}
		})
	}
}

// TestWrapperExec_EligibilityIsIdenticalAcrossShells pins the should_cd
// decision per shell, executed rather than string-matched.
//
// This exists because the fish block used bash's '[!-]*' bracket class,
// which fish's switch cannot evaluate -- it matches with wildcards only.
// Every branch name therefore fell through with should_cd unset and fish
// users never got the auto-cd at all. Text assertions on the generated
// source cannot see that; only running fish can.
func TestWrapperExec_EligibilityIsIdenticalAcrossShells(t *testing.T) {
	args := []struct {
		arg    string
		wantCd bool
	}{
		{"somebranch", true},
		{"feature/nested-name", true},
		{"add", true},
		{"init", true},
		{"clone", true},
		{"list", false},
		{"status", false},
		{"doctor", false},
		{"prune", false},
		{"env", false},
		{"--help", false},
		{"-h", false},
		{"--version", false},
		{"-v", false},
	}

	for _, sh := range []struct{ bin, shellType string }{
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
	} {
		t.Run(sh.shellType, func(t *testing.T) {
			bin := lookShell(t, sh.bin)

			for _, a := range args {
				t.Run(a.arg, func(t *testing.T) {
					fx := newWrapperFixture(t, wrapperFixtureOpts{})
					cwd, status := runWrapper(t, bin, sh.shellType, fx, a.arg)

					if status != 0 {
						t.Errorf("%s wrapper: exit status = %d, want 0", sh.shellType, status)
					}

					wantDir := resolve(t, fx.dir)
					if a.wantCd {
						wantDir = resolve(t, fx.worktree)
					}
					if cwd != wantDir {
						t.Errorf("%s wrapper: arg %q gave cwd %q, want %q (wantCd=%v)",
							sh.shellType, a.arg, cwd, wantDir, a.wantCd)
					}
				})
			}
		})
	}
}
