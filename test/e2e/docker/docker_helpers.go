package docker_test

import (
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"hop.top/git/internal/docker"
)

// SkipIfDockerNotAvailable checks if Docker is available and skips the test if not
func SkipIfDockerNotAvailable(t *testing.T) {
	t.Helper()

	d := docker.New()
	if !d.IsAvailable() {
		t.Skip("Docker is not available - skipping integration test")
	}

	// Also check if docker compose is available
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker Compose is not available - skipping integration test")
	}
}

// WaitForServiceHealthy waits for a service to become healthy
func WaitForServiceHealthy(t *testing.T, dir, service string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check container status
		cmd := exec.Command("docker", "compose", "ps", "--format", "json", service)
		cmd.Dir = dir
		output, err := cmd.Output()
		if err == nil && strings.Contains(string(output), "\"Health\":\"healthy\"") {
			t.Logf("Service %s is healthy", service)
			return
		}

		// Also check if service is just running (no healthcheck)
		if err == nil && strings.Contains(string(output), "\"State\":\"running\"") {
			// Give it a moment to stabilize
			time.Sleep(2 * time.Second)
			t.Logf("Service %s is running (no healthcheck defined)", service)
			return
		}

		time.Sleep(2 * time.Second)
	}

	// Get logs for debugging
	cmd := exec.Command("docker", "compose", "logs", service)
	cmd.Dir = dir
	logs, _ := cmd.Output()

	t.Fatalf("Service %s did not become healthy within %v\nLogs:\n%s", service, timeout, string(logs))
}

// CheckHTTPEndpoint checks if an HTTP endpoint is accessible
func CheckHTTPEndpoint(t *testing.T, url string, expectedStatus int) {
	t.Helper()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to access HTTP endpoint %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		t.Errorf("Expected status %d but got %d for %s", expectedStatus, resp.StatusCode, url)
	}

	t.Logf("HTTP endpoint %s is accessible (status: %d)", url, resp.StatusCode)
}

// CleanupContainers stops and removes all containers in the given directory
func CleanupContainers(t *testing.T, dir string) {
	t.Helper()

	d := docker.New()

	// Try to stop containers
	if err := d.ComposeStop(dir); err != nil {
		t.Logf("Warning: Failed to stop containers: %v", err)
	}

	// Try to remove containers and volumes
	if err := d.ComposeDown(dir); err != nil {
		t.Logf("Warning: Failed to cleanup containers: %v", err)
	}

	t.Logf("Cleaned up Docker containers in %s", dir)
}

// GetPortFromEnv reads a port value from the .env file
func GetPortFromEnv(t *testing.T, envContent, key string) string {
	t.Helper()

	for _, line := range strings.Split(envContent, "\n") {
		if strings.HasPrefix(line, key+"=") {
			port := strings.TrimPrefix(line, key+"=")
			port = strings.TrimSpace(port)
			if port == "" {
				t.Fatalf("Port value for %s is empty in .env", key)
			}
			return port
		}
	}

	t.Fatalf("Port key %s not found in .env file", key)
	return ""
}

// VerifyPortIsolation verifies that two ports are different
func VerifyPortIsolation(t *testing.T, port1, port2, label1, label2 string) {
	t.Helper()

	if port1 == port2 {
		t.Errorf("Ports should be different for %s and %s: both are %s", label1, label2, port1)
	} else {
		t.Logf("Port isolation verified: %s=%s, %s=%s", label1, port1, label2, port2)
	}
}

// WaitForContainerReady waits for container to be in running state
func WaitForContainerReady(t *testing.T, dir string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "compose", "ps", "--format", "json")
		cmd.Dir = dir
		output, err := cmd.Output()

		if err == nil {
			outputStr := string(output)
			// Check if we have at least one container running
			if strings.Contains(outputStr, "\"State\":\"running\"") {
				time.Sleep(1 * time.Second) // Give it a moment to stabilize
				return
			}
		}

		time.Sleep(1 * time.Second)
	}

	t.Fatalf("Containers did not start within %v", timeout)
}
