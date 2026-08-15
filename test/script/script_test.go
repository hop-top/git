// Package script runs the .txtar end-to-end suite.
//
// Each file under testdata/ is one scripted scenario: a sequence of shell-ish
// commands plus assertions on stdout, stderr, exit status and the on-disk
// tree. The scripts drive the real CLI against real git repositories — no
// mocks, no fakes — so a script failing means observable behaviour changed.
//
// Why scripts instead of more Go e2e tests: the Go tests in test/e2e spend
// most of their lines on process plumbing (build a binary, thread an env,
// capture streams, assert on substrings). A script says the same thing in a
// handful of lines and reads top-to-bottom like the session a user would
// actually type.
package script

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"hop.top/git/cmd"
)

// TestMain installs the CLI as an in-script command.
//
// testscript.Main runs the named function in a *separate process* (it
// re-executes this test binary with an internal marker), so the command sees
// a pristine global state and is free to call os.Exit — which cmd.Execute
// does on failure. The binary the script invokes is therefore this package's
// own build of the production entry point, not a stale artifact on $PATH:
// there is no `go build` step and nothing to go out of date.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"git-hop": func() {
			cmd.SetVersion("test", "none", "unknown")
			cmd.Execute()
		},
	})
}

// TestScripts runs every .txtar under testdata/ as a subtest named after the
// file, e.g. `go test ./test/script -run 'TestScripts/add_branch_slash'`.
func TestScripts(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata",
		RequireExplicitExec: true,
		UpdateScripts:       os.Getenv("UPDATE_SCRIPTS") != "",
		Setup:               setup,
	})
}

// setup isolates each script from the developer's real environment and from
// every other script.
//
// Everything the CLI reads for configuration is redirected under $WORK:
// XDG dirs, HOME, the git global config, and GIT_HOP_DATA_HOME (the
// hopspace root). Without this a script would allocate ports and write
// worktrees into the developer's actual hopspace.
//
// The git identity is pinned because scripts commit, and `init.defaultBranch`
// is pinned because assertions name `main` explicitly.
func setup(env *testscript.Env) error {
	work := env.WorkDir

	gitConfig := filepath.Join(work, "gitconfig")
	contents := "[user]\n\tname = Script User\n\temail = script@example.com\n" +
		"[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n" +
		"[protocol.file]\n\tallow = always\n"
	if err := os.WriteFile(gitConfig, []byte(contents), 0o644); err != nil {
		return err
	}

	env.Setenv("HOME", work)
	env.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	env.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(work, "gitconfig-system-absent"))
	env.Setenv("GIT_HOP_DATA_HOME", filepath.Join(work, "data"))
	env.Setenv("XDG_CONFIG_HOME", filepath.Join(work, ".config"))
	env.Setenv("XDG_DATA_HOME", filepath.Join(work, ".local", "share"))
	env.Setenv("XDG_STATE_HOME", filepath.Join(work, ".local", "state"))
	env.Setenv("XDG_CACHE_HOME", filepath.Join(work, ".cache"))

	// Docker is never exercised by these scripts; point the CLI at an empty
	// config so it cannot pick up the developer's credential helpers.
	env.Setenv("DOCKER_CONFIG", filepath.Join(work, "docker-config"))

	return nil
}
