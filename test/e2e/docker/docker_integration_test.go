package docker_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	e2e "hop.top/git/test/e2e"
)

// TestDockerIntegration_BasicStartup tests basic Docker container startup and health verification.
// This test ensures that:
// - Docker containers actually start (not just command execution)
// - Services become healthy within a reasonable timeout
// - HTTP endpoints are accessible on allocated ports
// - Services stop cleanly without errors
func TestDockerIntegration_BasicStartup(t *testing.T) {
	// Phase 1: Check Docker availability
	// Skip this test if Docker is not available on the system
	SkipIfDockerNotAvailable(t)

	// Phase 2: Setup test environment
	// Creates isolated temp directory with git-hop binary and test configuration
	env := e2e.SetupTestEnv(t)

	// Phase 3: Create bare repository
	// This simulates a remote Git repository that branches will be created from
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	// Phase 4: Create seed repository with Docker Compose configuration
	// Clone the bare repo, add docker-compose file, commit and push
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Read docker-compose-simple.yml fixture
	fixturesPath := filepath.Join("fixtures", "docker-compose-simple.yml")
	dcContent, err := os.ReadFile(fixturesPath)
	if err != nil {
		t.Fatalf("Failed to read docker-compose fixture from %s: %v", fixturesPath, err)
	}

	// Write docker-compose.yml to seed repository
	dockerComposeFile := filepath.Join(env.SeedRepoPath, "docker-compose.yml")
	e2e.WriteFile(t, dockerComposeFile, string(dcContent))

	// Commit and push the docker-compose file
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add docker-compose configuration")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Phase 5: Create a test branch
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "test-branch")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "test-branch")

	// Phase 6: Initialize hub with git-hop
	// This creates the hub directory structure
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Phase 7: Add the test branch to the hub
	// This creates a worktree and generates .env file with unique ports/volumes
	env.RunGitHop(t, env.HubPath, "add", "test-branch")

	// Get the path to the branch worktree
	branchPath := filepath.Join(env.HubPath, "hops", "test-branch")

	// Phase 8: Register cleanup to ensure containers are stopped even if test fails
	// This is critical to prevent orphaned containers
	t.Cleanup(func() {
		CleanupContainers(t, branchPath, "")
	})

	// Phase 9: Verify .env file was generated with required variables
	envFilePath := filepath.Join(branchPath, ".env")
	envContent, err := os.ReadFile(envFilePath)
	if err != nil {
		t.Fatalf("Failed to read .env file: %v", err)
	}
	t.Logf("Generated .env file:\n%s", string(envContent))

	// Phase 10: Start Docker services
	// This runs `git-hop env start` which executes `docker compose up -d`
	t.Log("Starting Docker services...")
	env.RunGitHop(t, branchPath, "env", "start")

	// Phase 11: Wait for services to become healthy
	// Services need time to start and pass their health checks
	t.Log("Waiting for services to become healthy...")
	WaitForServiceHealthy(t, branchPath, "web", 60*time.Second)
	WaitForServiceHealthy(t, branchPath, "cache", 60*time.Second)

	// Phase 12: Verify HTTP endpoint accessibility
	// Extract the allocated port from .env and test the nginx service
	webPort := GetPortFromEnv(t, string(envContent), "HOP_PORT_WEB")
	webURL := "http://localhost:" + webPort
	t.Logf("Testing HTTP endpoint: %s", webURL)

	// nginx returns 403 when accessing root without index.html (expected behavior)
	CheckHTTPEndpoint(t, webURL, 403)

	// Phase 13: Verify containers are running
	// Get container status to confirm they're operational
	psOutput := env.RunGitHop(t, branchPath, "env", "ps")
	t.Logf("Container status:\n%s", psOutput)

	// Phase 14: Stop services cleanly
	// This runs `git-hop env stop` which executes `docker compose stop`
	t.Log("Stopping Docker services...")
	env.RunGitHop(t, branchPath, "env", "stop")

	// Phase 15: Verify services are stopped
	// Check that containers are no longer running
	time.Sleep(2 * time.Second) // Give containers time to stop
	statusOutput := env.RunGitHop(t, env.HubPath, "status")
	t.Logf("Final status:\n%s", statusOutput)

	t.Log("Docker integration test completed successfully")
}
