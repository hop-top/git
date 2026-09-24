// Package testenv keeps test binaries off the invoking user's real
// directories.
//
// git-hop resolves its hopspaces, hooks, config, state, backups and
// shell caches from HOME and the XDG base directories, and shells out to
// git, which reads and writes the global git config. A test that forgets
// to redirect any one of those writes into the developer's real
// $XDG_DATA_HOME/git-hop, ~/.gitconfig, shell rc files and so on, and the
// debris accumulates run after run.
//
// Run fixes that for a whole test binary at once: before any test starts
// it points HOME, every XDG base directory and git's global config at a
// throwaway directory, and removes that directory when the tests finish.
// Tests that need a specific location still t.Setenv their own; this only
// changes the default a forgetful test falls back to. Every package with
// tests calls Run (or Wrap) from its TestMain, and
// TestEveryTestPackageIsolates fails when one does not.
package testenv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner is the part of *testing.M that Run needs. testscript.Main
// accepts the same shape, which is what makes Wrap possible.
type Runner interface {
	Run() int
}

var (
	root     string
	realHome string
)

// Root returns the throwaway directory the current test binary was
// isolated into, or "" when Run has not been called.
func Root() string { return root }

// RealHome returns the home directory the test binary was started with,
// before isolation replaced HOME. Tests that must reach a genuinely
// per-user resource (a docker CLI plugin, say) read it from here instead
// of os.UserHomeDir, which now points into the throwaway root.
func RealHome() string { return realHome }

// Run isolates the process environment, runs m, removes the throwaway
// directory and returns m's exit code. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }
//
// A test that re-executes its own binary as a helper process with an
// environment it built on purpose must have TestMain skip Run for that
// child; otherwise isolation replaces the environment the helper was
// handed. Children that simply inherit os.Environ are already isolated.
func Run(m Runner) int {
	dir, err := isolate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testenv: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)
	return m.Run()
}

// Wrap returns a Runner that calls Run(m). It is for TestMain functions
// that hand m to another harness, such as testscript.Main, which only
// calls m.Run in the parent test process and never in the subcommand
// processes it re-executes the binary as.
func Wrap(m Runner) Runner { return wrapped{m} }

type wrapped struct{ m Runner }

func (w wrapped) Run() int { return Run(w.m) }

// gitRepoVars locate a repository independently of the working
// directory. Git exports them to hooks, so a suite run from a git hook
// would otherwise aim every test's git commands at the real repository.
// git's own test harness unsets the same set.
var gitRepoVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
}

const testGitConfig = "[user]\n\tname = git-hop test\n\temail = git-hop-test@example.com\n"

func isolate() (string, error) {
	if home, err := os.UserHomeDir(); err == nil {
		realHome = home
	}

	// Must run before HOME and the XDG dirs move: the go command and the
	// docker CLI derive their defaults from them.
	pinGoToolchain()
	pinDockerConfig()

	dir, err := os.MkdirTemp("", "git-hop-test-")
	if err != nil {
		return "", err
	}
	// macOS hands out /var/folders/..., a symlink to /private/var/...;
	// resolving it keeps paths stable for tests that compare them.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	gitConfig := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(gitConfig, []byte(testGitConfig), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}

	set := map[string]string{
		"HOME":                filepath.Join(dir, "home"),
		"XDG_CONFIG_HOME":     filepath.Join(dir, "config"),
		"XDG_DATA_HOME":       filepath.Join(dir, "data"),
		"XDG_STATE_HOME":      filepath.Join(dir, "state"),
		"XDG_CACHE_HOME":      filepath.Join(dir, "cache"),
		"GIT_CONFIG_GLOBAL":   gitConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
	}
	for key, val := range set {
		if key != "GIT_CONFIG_GLOBAL" && key != "GIT_CONFIG_NOSYSTEM" {
			if err := os.MkdirAll(val, 0o755); err != nil {
				os.RemoveAll(dir)
				return "", err
			}
		}
		os.Setenv(key, val)
	}

	// GIT_HOP_DATA_HOME outranks XDG_DATA_HOME, so a value inherited from
	// the developer's shell would route hopspaces straight back to the
	// real data home. Unset rather than overridden, so tests that set
	// XDG_DATA_HOME alone still see hopspaces follow it.
	os.Unsetenv("GIT_HOP_DATA_HOME")
	for _, key := range gitRepoVars {
		os.Unsetenv(key)
	}

	root = dir
	return dir, nil
}

// pinGoToolchain fixes the go command's cache, module and env-file
// locations to what they resolve to for the real user. Tests that run
// `go build` would otherwise recompute them under the throwaway HOME and
// start from a cold build cache and an empty module cache.
func pinGoToolchain() {
	keys := []string{"GOPATH", "GOCACHE", "GOMODCACHE", "GOENV"}
	missing := false
	for _, key := range keys {
		if os.Getenv(key) == "" {
			missing = true
			break
		}
	}
	if !missing {
		return
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return
	}
	out, err := exec.Command(goBin, append([]string{"env", "-json"}, keys...)...).Output()
	if err != nil {
		return
	}
	var resolved map[string]string
	if json.Unmarshal(out, &resolved) != nil {
		return
	}
	for _, key := range keys {
		if os.Getenv(key) == "" && resolved[key] != "" {
			os.Setenv(key, resolved[key])
		}
	}
}

// pinDockerConfig keeps the docker CLI on the real user's contexts and CLI
// plugins. Docker is an external service, not git-hop state: tests that
// talk to it need the daemon the developer actually runs, which a docker
// config under the throwaway HOME would not know how to reach.
func pinDockerConfig() {
	if os.Getenv("DOCKER_CONFIG") != "" || realHome == "" {
		return
	}
	dir := filepath.Join(realHome, ".docker")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		os.Setenv("DOCKER_CONFIG", dir)
	}
}
