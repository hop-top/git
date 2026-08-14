package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"hop.top/git/internal/state"
)

var (
	sharedBinOnce sync.Once
	sharedBinPath string
	sharedBinErr  error
)

// ProjectRoot walks up from the test's working directory to the repo root.
// Tests run with the CWD set to their own package dir, so the number of
// levels depends on which package called.
func ProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	switch filepath.Base(dir) {
	case "e2e":
		return filepath.Dir(filepath.Dir(dir))
	case "docker":
		// test/e2e/docker
		return filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	}
	return dir
}

// SharedBinary builds the git-hop binary once per test binary and returns the
// path. The binary is byte-identical for every test, so rebuilding it per test
// only re-runs the linker against a fresh output path -- pure waste. The path
// is only ever executed, never mutated, so sharing it is safe.
//
// The binary lives outside any test's RootDir/HOME so tests that inspect their
// own temp tree never see it.
func SharedBinary(t *testing.T) string {
	t.Helper()
	sharedBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "git-hop-e2e-bin-*")
		if err != nil {
			sharedBinErr = err
			return
		}
		bin := filepath.Join(dir, "git-hop")
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "main.go")
		cmd.Dir = ProjectRoot(t)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			sharedBinErr = err
			t.Logf("go build failed: %v\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
			return
		}
		sharedBinPath = bin
	})
	if sharedBinErr != nil {
		t.Fatalf("Failed to build git-hop test binary: %v", sharedBinErr)
	}
	return sharedBinPath
}

// TestEnv holds the environment for an e2e test
type TestEnv struct {
	RootDir      string
	HubPath      string
	DataHome     string
	BinPath      string
	EnvVars      []string
	BareRepoPath string
	SeedRepoPath string
	StateHome    string
}

// LoadState reads this test's state.json directly from the test's own
// XDG_STATE_HOME instead of going through state.LoadState.
//
// state.LoadState resolves its path via hop.top/kit/go/core/xdg, which calls
// adrg/xdg.Reload() -- that repopulates PACKAGE-GLOBAL variables from the
// ambient process environment. Under t.Parallel() the ambient environment is
// shared by every running test, so no amount of per-test env threading can
// make that global resolve correctly per test. Reading the path we already
// know keeps the assertion identical while staying parallel-safe.
//
// Returns (nil, nil) when no state file exists, matching the "no state yet"
// case callers already handle.
func (e *TestEnv) LoadState(t *testing.T) (*state.State, error) {
	t.Helper()
	path := filepath.Join(e.StateHome, "git-hop", "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s state.State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SetupTestEnv creates a new isolated test environment
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	rootDir := CreateTempDir(t)
	t.Cleanup(func() {
		os.RemoveAll(rootDir)
	})

	bareRepoPath := filepath.Join(rootDir, "repo.git")
	seedRepoPath := filepath.Join(rootDir, "seed")
	hubPath := filepath.Join(rootDir, "hub")
	dataHome := filepath.Join(rootDir, "data")
	binPath := SharedBinary(t)

	gitConfigPath := filepath.Join(rootDir, "gitconfig")
	WriteFile(t, gitConfigPath, "[user]\n\tname = Test User\n\temail = test@example.com\n[init]\n\tdefaultBranch = main\n")

	dockerConfigDir := filepath.Join(rootDir, "docker-config")
	cliPluginsDir := filepath.Join(dockerConfigDir, "cli-plugins")
	os.MkdirAll(cliPluginsDir, 0755)
	WriteFile(t, filepath.Join(dockerConfigDir, "config.json"), "{}")

	home, err := os.UserHomeDir()
	if err == nil {
		pluginPath := filepath.Join(home, ".docker", "cli-plugins", "docker-compose")
		if _, err := os.Stat(pluginPath); err == nil {
			os.Symlink(pluginPath, filepath.Join(cliPluginsDir, "docker-compose"))
		}
	}

	xdgConfigHome := filepath.Join(rootDir, ".config")
	xdgDataHome := filepath.Join(rootDir, ".local", "share")
	xdgStateHome := filepath.Join(rootDir, ".local", "state")

	envVars := []string{
		// Scratch dir for test hook scripts to drop marker files into. Hooks
		// used to hardcode shared /tmp paths, which two concurrent runs of the
		// same test (e.g. -count=2) would clobber; anchoring it to this test's
		// own RootDir makes marker writes private per test.
		"GIT_HOP_TEST_MARKER_DIR=" + filepath.Join(rootDir, "markers"),
		"GIT_HOP_DATA_HOME=" + dataHome,
		"PATH=" + os.Getenv("PATH"),
		"DOCKER_CONFIG=" + dockerConfigDir,
		"GIT_CONFIG_GLOBAL=" + gitConfigPath,
		"HOME=" + rootDir,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
		"XDG_DATA_HOME=" + xdgDataHome,
		"XDG_STATE_HOME=" + xdgStateHome,
	}

	// Environment is threaded EXPLICITLY through EnvVars and applied to each
	// exec.Command below -- never mirrored into the parent test process via
	// t.Setenv. Process-global env is shared by every concurrently running
	// test, so mutating it is both a cross-test leak and an outright bar to
	// t.Parallel() (the runtime panics on t.Setenv + t.Parallel).
	//
	// Parent-process assertions that used to depend on those globals now read
	// the paths recorded on TestEnv directly -- see TestEnv.LoadState.

	return &TestEnv{
		RootDir:      rootDir,
		HubPath:      hubPath,
		DataHome:     dataHome,
		BinPath:      binPath,
		EnvVars:      envVars,
		BareRepoPath: bareRepoPath,
		SeedRepoPath: seedRepoPath,
		StateHome:    xdgStateHome,
	}
}

// RunCommandAllowFail runs a command and returns stdout + error (does not fatal).
func (e *TestEnv) RunCommandAllowFail(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = e.EnvVars
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	t.Logf("Running (allow-fail): %s %v in %s", name, args, dir)
	err := cmd.Run()
	if err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}

// RunCommandWithExit runs a command and returns stdout, stderr, and exit code.
func (e *TestEnv) RunCommandWithExit(t *testing.T, dir, name string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = e.EnvVars
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	t.Logf("Running: %s %v in %s", name, args, dir)
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Logf("exec error (not ExitError): %v", err)
			exitCode = -1
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// RunGitHop runs the git-hop binary with the test environment
func (e *TestEnv) RunGitHop(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return e.RunCommand(t, dir, e.BinPath, args...)
}

// RunGitHopCombined runs the git-hop binary and returns combined stdout+stderr.
// Does not fatal on non-zero exit; use when testing warning/error output.
func (e *TestEnv) RunGitHopCombined(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(e.BinPath, args...)
	cmd.Dir = dir
	cmd.Env = e.EnvVars
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	t.Logf("Running: %s %v in %s", e.BinPath, args, dir)
	_ = cmd.Run()
	return buf.String()
}

// RunCommand runs a command in the given directory with the test environment
func (e *TestEnv) RunCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = e.EnvVars

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Running: %s %v in %s", name, args, dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command %s %v failed: %v\nStdout: %s\nStderr: %s", name, args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// RunCommand runs a command in the given directory
func RunCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Running: %s %v in %s", name, args, dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command failed: %s %v\nError: %v\nStdout: %s\nStderr: %s", name, args, err, stdout.String(), stderr.String())
	}
}

// RunCommandOutput runs a command and returns stdout
func RunCommandOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Running: %s %v in %s", name, args, dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command failed: %s %v\nError: %v\nStdout: %s\nStderr: %s", name, args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// CreateTempDir creates a temporary directory for the test
func CreateTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "git-hop-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return dir
}

// SkipIfDockerNotAvailable skips the test if Docker daemon is not running.
// Use this at the top of any test that requires a live Docker daemon.
func SkipIfDockerNotAvailable(t *testing.T) {
	t.Helper()
	// docker info fails when daemon is not running (unlike docker version which just checks the CLI)
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker daemon is not running - skipping test")
	}
	cmd = exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker Compose is not available - skipping test")
	}
}

// WriteFile writes content to a file
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", path, err)
	}
}
