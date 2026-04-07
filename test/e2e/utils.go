package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEnv holds the environment for an e2e test
type TestEnv struct {
	RootDir      string
	HubPath      string
	DataHome     string
	BinPath      string
	EnvVars      []string
	BareRepoPath string
	SeedRepoPath string
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
	binPath := filepath.Join(rootDir, "git-hop")

	projectRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	if filepath.Base(projectRoot) == "e2e" {
		projectRoot = filepath.Dir(filepath.Dir(projectRoot))
	} else if filepath.Base(projectRoot) == "docker" {
		// Handle test/e2e/docker subdirectory
		projectRoot = filepath.Dir(filepath.Dir(filepath.Dir(projectRoot)))
	}
	RunCommand(t, projectRoot, "go", "build", "-o", binPath, "main.go")

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
		"GIT_HOP_DATA_HOME=" + dataHome,
		"PATH=" + os.Getenv("PATH"),
		"DOCKER_CONFIG=" + dockerConfigDir,
		"GIT_CONFIG_GLOBAL=" + gitConfigPath,
		"HOME=" + rootDir,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
		"XDG_DATA_HOME=" + xdgDataHome,
		"XDG_STATE_HOME=" + xdgStateHome,
	}

	// Mirror the same overrides into the PARENT test process. The child
	// git-hop binary picks them up via cmd.Env above, but the test
	// process itself also calls helpers like state.LoadState(afero.OsFs)
	// which read os.Getenv directly. Without these, the parent reads
	// the developer's REAL ~/.config, ~/.local/share, and macOS
	// fallback state path — leaking pre-existing entries into the test
	// and producing flaky pass/fail based on host state.
	//
	// t.Setenv auto-restores on test completion. None of the e2e tests
	// use t.Parallel() (as of 2026-04-07), so it's safe here.
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	t.Setenv("HOME", rootDir)
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	t.Setenv("XDG_STATE_HOME", xdgStateHome)

	return &TestEnv{
		RootDir:      rootDir,
		HubPath:      hubPath,
		DataHome:     dataHome,
		BinPath:      binPath,
		EnvVars:      envVars,
		BareRepoPath: bareRepoPath,
		SeedRepoPath: seedRepoPath,
	}
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

// StopDockerEnv stops and removes Docker containers in dir.
// Safe to call even if containers are not running; errors are logged, not fatal.
func StopDockerEnv(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	for _, args := range [][]string{
		{"compose", "stop"},
		{"compose", "down", "--volumes", "--remove-orphans"},
	} {
		cmd := exec.Command("docker", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("docker %v in %s: %v\n%s", args, dir, err, out)
		}
	}
}
