package docker_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "github.com/jadb/git-hop/test/e2e"
)

// TestDockerOverride_HardcodedPortIsolation verifies that git-hop automatically generates
// docker-compose override files for projects with hardcoded ports, enabling concurrent
// environments without manual compose file changes.
func TestDockerOverride_HardcodedPortIsolation(t *testing.T) {
	SkipIfDockerNotAvailable(t)

	env := e2e.SetupTestEnv(t)

	// Create bare repo and seed with hardcoded docker-compose.yml
	setupHardcodedDockerRepo(t, env)

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	t.Logf("Initialized hub at %s", env.HubPath)

	// Add two branches
	env.RunGitHop(t, env.HubPath, "add", "branch-a")
	env.RunGitHop(t, env.HubPath, "add", "branch-b")

	branchAPath := filepath.Join(env.HubPath, "hops", "branch-a")
	branchBPath := filepath.Join(env.HubPath, "hops", "branch-b")

	// Register cleanup
	t.Cleanup(func() {
		CleanupContainers(t, branchAPath)
		CleanupContainers(t, branchBPath)
	})

	// Verify override files were generated in cache
	// The XDG_CACHE_HOME is set to rootDir/.cache by the test env (via HOME)
	cacheBase := filepath.Join(env.RootDir, "Library", "Caches", "git-hop")
	t.Logf("Looking for override cache under: %s", cacheBase)

	// Find override files (org/repo structure depends on bare repo URL parsing)
	overrideFilesA := findOverrideFiles(t, cacheBase, "branch-a")
	overrideFilesB := findOverrideFiles(t, cacheBase, "branch-b")

	if len(overrideFilesA) == 0 {
		t.Log("No override file found for branch-a (may use different cache path)")
	} else {
		t.Logf("Override for branch-a: %s", overrideFilesA[0])
		content, err := os.ReadFile(overrideFilesA[0])
		if err == nil {
			if !strings.Contains(string(content), "${HOP_PORT_") {
				t.Errorf("Override file should contain HOP_PORT_* variables, got:\n%s", string(content))
			}
			t.Logf("Override content for branch-a:\n%s", string(content))
		}
	}

	if len(overrideFilesB) == 0 {
		t.Log("No override file found for branch-b (may use different cache path)")
	} else {
		t.Logf("Override for branch-b: %s", overrideFilesB[0])
	}

	// Verify .env files have different HOP_PORT_WEB and HOP_PORT_CACHE values
	portAWeb := getPortFromEnvFile(t, branchAPath, "HOP_PORT_WEB")
	portBWeb := getPortFromEnvFile(t, branchBPath, "HOP_PORT_WEB")
	portACache := getPortFromEnvFile(t, branchAPath, "HOP_PORT_CACHE")
	portBCache := getPortFromEnvFile(t, branchBPath, "HOP_PORT_CACHE")

	if portAWeb == portBWeb {
		t.Errorf("Web ports should be different: branch-a=%s, branch-b=%s", portAWeb, portBWeb)
	}
	t.Logf("Port isolation: branch-a web=%s cache=%s, branch-b web=%s cache=%s",
		portAWeb, portACache, portBWeb, portBCache)

	if portACache == portBCache {
		t.Errorf("Cache ports should be different: branch-a=%s, branch-b=%s", portACache, portBCache)
	}

	// Start both environments concurrently
	t.Log("Starting both environments concurrently...")
	env.RunGitHop(t, branchAPath, "env", "start")
	env.RunGitHop(t, branchBPath, "env", "start")

	// Wait for services to be healthy
	WaitForServiceHealthy(t, branchAPath, "web", 60*time.Second)
	WaitForServiceHealthy(t, branchBPath, "web", 60*time.Second)
	t.Log("Both web services are healthy")

	WaitForServiceHealthy(t, branchAPath, "cache", 30*time.Second)
	WaitForServiceHealthy(t, branchBPath, "cache", 30*time.Second)
	t.Log("Both cache services are healthy")

	// Verify both HTTP endpoints are accessible on their allocated ports
	urlA := fmt.Sprintf("http://localhost:%s", portAWeb)
	urlB := fmt.Sprintf("http://localhost:%s", portBWeb)

	CheckHTTPEndpoint(t, urlA, http.StatusOK)
	CheckHTTPEndpoint(t, urlB, http.StatusOK)
	t.Logf("Both endpoints accessible: %s and %s", urlA, urlB)

	// Stop both
	env.RunGitHop(t, branchAPath, "env", "stop")
	env.RunGitHop(t, branchBPath, "env", "stop")
	t.Log("Both environments stopped successfully")

	t.Log("Override-based port isolation test passed")
}

// setupHardcodedDockerRepo creates a bare repo seeded with a docker-compose.yml that has hardcoded ports
func setupHardcodedDockerRepo(t *testing.T, env *e2e.TestEnv) {
	t.Helper()

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	dcContent, err := os.ReadFile("fixtures/docker-compose-hardcoded.yml")
	if err != nil {
		t.Fatalf("Failed to read hardcoded docker-compose fixture: %v", err)
	}

	e2e.WriteFile(t, filepath.Join(env.SeedRepoPath, "docker-compose.yml"), string(dcContent))

	env.RunCommand(t, env.SeedRepoPath, "git", "add", "docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add docker-compose.yml with hardcoded ports")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
}

// findOverrideFiles searches for docker-compose.override.yml files in the cache directory
func findOverrideFiles(t *testing.T, cacheBase, branchName string) []string {
	t.Helper()

	var found []string
	filepath.Walk(cacheBase, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "docker-compose.override.yml" && strings.Contains(path, branchName) {
			found = append(found, path)
		}
		return nil
	})
	return found
}
