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

	envVars := []string{
		"GIT_HOP_DATA_HOME=" + dataHome,
		"PATH=" + os.Getenv("PATH"),
		"DOCKER_CONFIG=" + dockerConfigDir,
		"GIT_CONFIG_GLOBAL=" + gitConfigPath,
		"HOME=" + rootDir,
		"XDG_CONFIG_HOME=" + filepath.Join(rootDir, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(rootDir, ".local", "share"),
	}

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

// WriteFile writes content to a file
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", path, err)
	}
}
