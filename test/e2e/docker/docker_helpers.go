//go:build dockere2e

package docker_test

import (
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/services"
)

// hopComposeProject derives the docker compose project name hop uses for a
// given worktree + branch. Returns "" if the enclosing hub config can't be
// loaded, which falls back to compose's default (cwd basename). Callers
// thread this into -p flags so test helpers target the same containers
// hop started.
func hopComposeProject(dir, branch string) string {
	fs := afero.NewOsFs()
	hubPath, err := hop.FindHub(fs, dir)
	if err != nil {
		return ""
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return ""
	}
	return services.ComposeProjectName(hub.Config.Repo.Org, hub.Config.Repo.Repo, branch)
}

// composeArgs returns the leading `compose -p <project>` args for a docker
// exec.Command invocation; when project is empty, falls back to a bare
// `compose` (default project = cwd basename).
func composeArgs(project string, rest ...string) []string {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "-p", project)
	}
	return append(args, rest...)
}

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

// WaitForServiceHealthy waits for a service to become healthy. branch is the
// hop branch used to derive the compose project name so this targets the
// containers hop started (which now run under `docker compose -p <project>`).
func WaitForServiceHealthy(t *testing.T, dir, branch, service string, timeout time.Duration) {
	t.Helper()

	project := hopComposeProject(dir, branch)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check container status
		cmd := exec.Command("docker", composeArgs(project, "ps", "--format", "json", service)...)
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
	cmd := exec.Command("docker", composeArgs(project, "logs", service)...)
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

// CleanupContainers stops and removes containers started by `git hop env
// start` for the given worktree + branch. The branch is used to derive the
// same compose project name hop injects via -p, so this helper targets the
// correct project even when multiple hops share branch names across hubs.
//
// If the enclosing hub can't be loaded, the project name falls back to
// empty (compose's default, basename of dir) — same behavior as before the
// project-isolation change.
func CleanupContainers(t *testing.T, dir, branch string) {
	t.Helper()

	project := hopComposeProject(dir, branch)
	d := docker.New()

	// Try to stop containers
	if err := d.ComposeStop(dir, project); err != nil {
		t.Logf("Warning: Failed to stop containers: %v", err)
	}

	// Try to remove containers and associated compose resources. Note:
	// this does not pass --volumes, so named volumes survive cleanup; tests
	// that need volume teardown must use docker compose down --volumes
	// directly.
	if err := d.ComposeDown(dir, project); err != nil {
		t.Logf("Warning: Failed to cleanup containers: %v", err)
	}

	t.Logf("Cleaned up Docker containers in %s (project=%q)", dir, project)
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

// WaitForContainerReady waits for container to be in running state.
// branch is the hop branch used to derive the compose project name so this
// targets the containers hop started.
func WaitForContainerReady(t *testing.T, dir, branch string, timeout time.Duration) {
	t.Helper()

	project := hopComposeProject(dir, branch)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", composeArgs(project, "ps", "--format", "json")...)
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
