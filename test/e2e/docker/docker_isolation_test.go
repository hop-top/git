//go:build dockere2e

package docker_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hop.top/git/test/e2e"
)

// TestDockerIsolation_PortIsolation verifies that different branches get different ports
// and both environments can run concurrently without conflicts.
func TestDockerIsolation_PortIsolation(t *testing.T) {
	SkipIfDockerNotAvailable(t)

	// Setup test environment
	env := e2e.SetupTestEnv(t)

	// Create bare repo and seed with docker-compose.yml
	setupDockerRepo(t, env)

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Add two branches
	env.RunGitHop(t, env.HubPath, "add", "branch-a")
	env.RunGitHop(t, env.HubPath, "add", "branch-b")

	branchAPath := filepath.Join(env.HubPath, "hops", "branch-a")
	branchBPath := filepath.Join(env.HubPath, "hops", "branch-b")

	// Register cleanup for both environments
	t.Cleanup(func() {
		CleanupContainers(t, branchAPath, "branch-a")
		CleanupContainers(t, branchBPath, "branch-b")
	})

	// Start both environments concurrently
	env.RunGitHop(t, branchAPath, "env", "start")
	env.RunGitHop(t, branchBPath, "env", "start")

	// Get ports from .env files
	portA := getPortFromEnvFile(t, branchAPath, "HOP_PORT_WEB")
	portB := getPortFromEnvFile(t, branchBPath, "HOP_PORT_WEB")

	// Verify different ports allocated
	if portA == portB {
		t.Fatalf("Both branches allocated same port: %s", portA)
	}
	t.Logf("Branch A port: %s, Branch B port: %s", portA, portB)

	// Verify both services are healthy
	WaitForServiceHealthy(t, branchAPath, "branch-a", "web", 30*time.Second)
	WaitForServiceHealthy(t, branchBPath, "branch-b", "web", 30*time.Second)
	t.Log("Both services are healthy")

	// Verify both HTTP endpoints are accessible
	urlA := fmt.Sprintf("http://localhost:%s", portA)
	urlB := fmt.Sprintf("http://localhost:%s", portB)

	CheckHTTPEndpoint(t, urlA, http.StatusOK)
	CheckHTTPEndpoint(t, urlB, http.StatusOK)
	t.Logf("Both endpoints accessible: %s and %s", urlA, urlB)

	// Stop both environments
	env.RunGitHop(t, branchAPath, "env", "stop")
	env.RunGitHop(t, branchBPath, "env", "stop")
	t.Log("Both environments stopped successfully")
}

// TestDockerIsolation_VolumeDataIsolation verifies that volume data is isolated
// between branches and persists across restarts.
func TestDockerIsolation_VolumeDataIsolation(t *testing.T) {
	SkipIfDockerNotAvailable(t)

	// Setup test environment
	env := e2e.SetupTestEnv(t)

	// Create bare repo and seed with docker-compose.yml
	setupDockerRepo(t, env)

	// Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Add two branches
	env.RunGitHop(t, env.HubPath, "add", "branch-x")
	env.RunGitHop(t, env.HubPath, "add", "branch-y")

	branchXPath := filepath.Join(env.HubPath, "hops", "branch-x")
	branchYPath := filepath.Join(env.HubPath, "hops", "branch-y")

	// Register cleanup for both environments
	t.Cleanup(func() {
		CleanupContainers(t, branchXPath, "branch-x")
		CleanupContainers(t, branchYPath, "branch-y")
	})

	// Get volume paths from .env files
	volumeXPath := getVolumePathFromEnvFile(t, branchXPath, "HOP_VOLUME_WEB_DATA")
	volumeYPath := getVolumePathFromEnvFile(t, branchYPath, "HOP_VOLUME_WEB_DATA")

	// Verify volumes are different
	if volumeXPath == volumeYPath {
		t.Fatalf("Both branches use same volume path: %s", volumeXPath)
	}
	t.Logf("Branch X volume: %s", volumeXPath)
	t.Logf("Branch Y volume: %s", volumeYPath)

	// Write different test data to each volume
	dataX := "<html><body><h1>Branch X Data</h1></body></html>"
	dataY := "<html><body><h1>Branch Y Data</h1></body></html>"

	writeVolumeData(t, volumeXPath, "index.html", dataX)
	writeVolumeData(t, volumeYPath, "index.html", dataY)
	t.Log("Written different data to each volume")

	// Start both services
	env.RunGitHop(t, branchXPath, "env", "start")
	env.RunGitHop(t, branchYPath, "env", "start")

	// Wait for services to be healthy
	WaitForServiceHealthy(t, branchXPath, "branch-x", "web", 30*time.Second)
	WaitForServiceHealthy(t, branchYPath, "branch-y", "web", 30*time.Second)

	// Get ports for HTTP requests
	portX := getPortFromEnvFile(t, branchXPath, "HOP_PORT_WEB")
	portY := getPortFromEnvFile(t, branchYPath, "HOP_PORT_WEB")

	// Verify each service serves its own data
	contentX := getHTTPContent(t, fmt.Sprintf("http://localhost:%s", portX))
	contentY := getHTTPContent(t, fmt.Sprintf("http://localhost:%s", portY))

	if !strings.Contains(contentX, "Branch X Data") {
		t.Errorf("Branch X not serving correct data. Got: %s", contentX)
	}
	if !strings.Contains(contentY, "Branch Y Data") {
		t.Errorf("Branch Y not serving correct data. Got: %s", contentY)
	}
	t.Log("Both services serving their own isolated data")

	// Test persistence: Stop branch-x, restart it, verify data still there
	env.RunGitHop(t, branchXPath, "env", "stop")
	t.Log("Stopped branch-x")

	// Give it a moment to fully stop
	time.Sleep(2 * time.Second)

	env.RunGitHop(t, branchXPath, "env", "start")
	WaitForServiceHealthy(t, branchXPath, "branch-x", "web", 30*time.Second)
	t.Log("Restarted branch-x")

	// Verify data persisted
	contentX = getHTTPContent(t, fmt.Sprintf("http://localhost:%s", portX))
	if !strings.Contains(contentX, "Branch X Data") {
		t.Errorf("Branch X data did not persist after restart. Got: %s", contentX)
	}
	t.Log("Data persisted across restart")

	// Stop both environments
	env.RunGitHop(t, branchXPath, "env", "stop")
	env.RunGitHop(t, branchYPath, "env", "stop")
	t.Log("Both environments stopped successfully")
}

// Helper Functions

// getHTTPContent fetches the content from an HTTP URL and returns it as a string.
func getHTTPContent(t *testing.T, url string) string {
	t.Helper()

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body from %s: %v", url, err)
	}

	return string(body)
}

// setupDockerRepo creates a bare repo and seeds it with docker-compose.yml
func setupDockerRepo(t *testing.T, env *e2e.TestEnv) {
	t.Helper()

	// Create bare repo
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	// Clone to seed repo
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Read docker-compose.yml fixture
	dcContent, err := os.ReadFile("fixtures/docker-compose-simple.yml")
	if err != nil {
		t.Fatalf("Failed to read docker-compose fixture: %v", err)
	}

	// Write to seed repo
	e2e.WriteFile(t, filepath.Join(env.SeedRepoPath, "docker-compose.yml"), string(dcContent))

	// Commit and push
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add docker-compose.yml")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
}

// getPortFromEnvFile reads a port value from the .env file in the specified directory.
func getPortFromEnvFile(t *testing.T, dir, key string) string {
	t.Helper()

	envPath := filepath.Join(dir, ".env")
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("Failed to read .env from %s: %v", dir, err)
	}

	return GetPortFromEnv(t, string(content), key)
}

// getVolumePathFromEnvFile reads a volume path from the .env file in the specified directory.
func getVolumePathFromEnvFile(t *testing.T, dir, key string) string {
	t.Helper()

	envPath := filepath.Join(dir, ".env")
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("Failed to read .env from %s: %v", dir, err)
	}

	// Parse like GetPortFromEnv
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, key+"=") {
			path := strings.TrimPrefix(line, key+"=")
			path = strings.TrimSpace(path)
			if path == "" {
				t.Fatalf("Volume path for %s is empty in .env", key)
			}
			return path
		}
	}

	t.Fatalf("Volume key %s not found in .env file at %s", key, envPath)
	return ""
}

// writeVolumeData writes data to a file in the specified volume directory.
func writeVolumeData(t *testing.T, volumePath, filename, content string) {
	t.Helper()

	// Ensure volume directory exists
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		t.Fatalf("Failed to create volume directory %s: %v", volumePath, err)
	}

	// Write content to file
	filePath := filepath.Join(volumePath, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write to %s: %v", filePath, err)
	}

	t.Logf("Wrote data to %s", filePath)
}
