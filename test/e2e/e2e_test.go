package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_PortAndVolumeIsolation(t *testing.T) {
	SkipIfDockerNotAvailable(t)

	// Setup
	env := SetupTestEnv(t)

	// 1. Create Bare Repo
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	// 2. Seed Repo
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Read docker-compose.yml fixture
	dcContent, err := os.ReadFile("fixtures/docker-compose.yml")
	if err != nil {
		t.Fatalf("Failed to read docker-compose fixture: %v", err)
	}
	dockerComposeContent := string(dcContent)
	WriteFile(t, filepath.Join(env.SeedRepoPath, "docker-compose.yml"), dockerComposeContent)

	env.RunCommand(t, env.SeedRepoPath, "git", "add", "docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Create branches in seed repo so they exist for git-hop to add
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "branch-a")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "branch-a")
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "branch-b")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "branch-b")

	// 3. Initialize Hub using git hop clone
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// 4. Add Branches
	env.RunGitHop(t, env.HubPath, "add", "branch-a")
	env.RunGitHop(t, env.HubPath, "add", "branch-b")

	// 5. Verify .env generation
	branchAPath := filepath.Join(env.HubPath, "hops", "branch-a")
	branchBPath := filepath.Join(env.HubPath, "hops", "branch-b")

	// Register cleanup early so containers are stopped even on test failure
	t.Cleanup(func() {
		StopDockerEnv(t, branchAPath)
		StopDockerEnv(t, branchBPath)
	})

	checkEnv := func(path string) {
		content, err := os.ReadFile(filepath.Join(path, ".env"))
		if err != nil {
			t.Fatalf("Failed to read .env in %s: %v", path, err)
		}
		s := string(content)
		if !strings.Contains(s, "HOP_PORT_WEB=") {
			t.Errorf(".env in %s missing HOP_PORT_WEB", path)
		}
		if !strings.Contains(s, "HOP_VOLUME_WEB_DATA=") {
			t.Errorf(".env in %s missing HOP_VOLUME_WEB_DATA", path)
		}
	}
	checkEnv(branchAPath)
	checkEnv(branchBPath)

	// 6. Start Environments
	env.RunGitHop(t, filepath.Join(env.HubPath, "hops", "branch-a"), "env", "start")
	env.RunGitHop(t, filepath.Join(env.HubPath, "hops", "branch-b"), "env", "start")

	// 7. Verify Isolation
	statusOut := env.RunGitHop(t, env.HubPath, "status")
	t.Logf("Status Output:\n%s", statusOut)

	getEnvVar := func(path, key string) string {
		content, _ := os.ReadFile(filepath.Join(path, ".env"))
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, key+"=") {
				return strings.TrimPrefix(line, key+"=")
			}
		}
		return ""
	}

	portA := getEnvVar(branchAPath, "HOP_PORT_WEB")
	portB := getEnvVar(branchBPath, "HOP_PORT_WEB")

	if portA == portB {
		t.Errorf("Ports should be different: %s vs %s", portA, portB)
	}

}
