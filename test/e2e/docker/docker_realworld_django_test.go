package docker_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "github.com/jadb/git-hop/test/e2e"
)

// TestDockerRealWorld_Django tests a real-world Django application with Docker Compose.
//
// This test demonstrates git-hop's ability to manage multiple production-grade Django
// environments with complex service dependencies including:
// - Django web application
// - PostgreSQL database
// - Redis for caching/message broker
// - Celery workers (if available in the repository)
//
// The test verifies:
// 1. Cloning a real OSS Django application from GitHub
// 2. Port and volume isolation between production and development branches
// 3. Independent lifecycle management (start/stop/restart)
// 4. Database and cache isolation between environments
// 5. Production stability while development environment is modified
//
// Target repository: wagtail/bakerydemo (Wagtail CMS demo with Docker)
// Fallback: django-cookiecutter or similar production Django apps with docker-compose
func TestDockerRealWorld_Django(t *testing.T) {
	// Phase 1: Verify Docker availability
	SkipIfDockerNotAvailable(t)

	// Phase 2: Setup test environment
	env := e2e.SetupTestEnv(t)

	// Phase 3: Clone real Django application
	// Using Wagtail's bakerydemo - a production-ready Django/Wagtail app with Docker
	repoURL := "https://github.com/wagtail/bakerydemo.git"

	t.Logf("Cloning real Django application from %s...", repoURL)
	cloneCmd := exec.Command("git", "clone", "--bare", "--depth", "1", repoURL, env.BareRepoPath)
	cloneCmd.Dir = env.RootDir

	// Set timeout for clone operation (network dependent)
	if err := cloneCmd.Run(); err != nil {
		// If bakerydemo fails, try alternative Django repo
		t.Logf("Failed to clone bakerydemo, trying alternative repository...")
		repoURL = "https://github.com/django/djangoproject.com.git"

		cloneCmd = exec.Command("git", "clone", "--bare", "--depth", "1", repoURL, env.BareRepoPath)
		cloneCmd.Dir = env.RootDir

		if err := cloneCmd.Run(); err != nil {
			t.Skipf("Failed to clone Django app (network issue or repo unavailable): %v", err)
		}
	}
	t.Logf("Successfully cloned repository")

	// Phase 4: Initialize hub
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	t.Logf("Initialized hub at %s", env.HubPath)

	// Phase 5: Add production branch
	// Most Django repos use 'main' or 'master' as primary branch
	t.Log("Adding production branch...")
	env.RunGitHop(t, env.HubPath, "add", "main")
	prodPath := filepath.Join(env.HubPath, "hops", "main")

	// Phase 6: Verify docker-compose.yml exists
	dockerComposePath := filepath.Join(prodPath, "docker-compose.yml")
	if _, err := os.Stat(dockerComposePath); err != nil {
		// Try docker-compose.yaml
		dockerComposePath = filepath.Join(prodPath, "docker-compose.yaml")
		if _, err := os.Stat(dockerComposePath); err != nil {
			t.Skipf("No docker-compose.yml found in repository - cannot test Docker functionality")
		}
	}
	t.Logf("Found docker-compose configuration: %s", dockerComposePath)

	// Phase 7: Create development branch from production
	t.Log("Creating development branch...")
	env.RunCommand(t, prodPath, "git", "checkout", "-b", "development")
	env.RunCommand(t, prodPath, "git", "push", "origin", "development")

	// Phase 8: Add development branch to hub
	t.Log("Adding development branch to hub...")
	env.RunGitHop(t, env.HubPath, "add", "development")
	devPath := filepath.Join(env.HubPath, "hops", "development")

	// Phase 9: Register cleanup to ensure containers are stopped
	t.Cleanup(func() {
		t.Log("Cleaning up Docker containers...")
		CleanupContainers(t, prodPath)
		CleanupContainers(t, devPath)
	})

	// Phase 10: Read and analyze docker-compose.yml to identify services
	dcContent, err := os.ReadFile(dockerComposePath)
	if err != nil {
		t.Fatalf("Failed to read docker-compose.yml: %v", err)
	}

	// Identify primary web service (common names: web, app, django, backend)
	webService := identifyWebService(string(dcContent))
	if webService == "" {
		t.Log("Warning: Could not identify web service, using 'web' as default")
		webService = "web"
	}
	t.Logf("Identified web service: %s", webService)

	// Phase 11: Start production environment
	t.Log("==== Starting production environment ====")
	env.RunGitHop(t, prodPath, "env", "start")

	// Phase 12: Wait for production services
	// Django apps can take significant time to start (pip install, migrations, collectstatic)
	t.Log("Waiting for production services to become healthy (this may take several minutes)...")

	// Wait for web service with extended timeout for Django startup
	WaitForServiceHealthy(t, prodPath, webService, 300*time.Second)

	// If database service exists, wait for it too
	if strings.Contains(string(dcContent), "postgres") || strings.Contains(string(dcContent), "db:") {
		WaitForServiceHealthy(t, prodPath, "db", 60*time.Second)
		t.Log("Database service is healthy")
	}

	// Phase 13: Start development environment concurrently
	t.Log("==== Starting development environment ====")
	env.RunGitHop(t, devPath, "env", "start")

	// Wait for development services
	t.Log("Waiting for development services to become healthy...")
	WaitForServiceHealthy(t, devPath, webService, 300*time.Second)

	// Phase 14: Verify port isolation
	t.Log("==== Verifying port isolation ====")
	portProd := getPortFromEnvFile(t, prodPath, "HOP_PORT_WEB")
	portDev := getPortFromEnvFile(t, devPath, "HOP_PORT_WEB")

	if portProd == portDev {
		t.Errorf("FAIL: Production and development allocated same port: %s", portProd)
	} else {
		t.Logf("SUCCESS: Port isolation verified - prod:%s, dev:%s", portProd, portDev)
	}

	// Phase 15: Test HTTP accessibility
	t.Log("==== Testing HTTP endpoints ====")
	urlProd := fmt.Sprintf("http://localhost:%s", portProd)
	urlDev := fmt.Sprintf("http://localhost:%s", portDev)

	// Django apps may return various status codes depending on configuration
	// 200 (OK), 302 (redirect), or 404 (not found) are all acceptable for this test
	testDjangoEndpoint(t, urlProd, "production")
	testDjangoEndpoint(t, urlDev, "development")

	t.Logf("✓ Production accessible at %s", urlProd)
	t.Logf("✓ Development accessible at %s", urlDev)

	// Phase 16: Verify database isolation
	if strings.Contains(string(dcContent), "postgres") || strings.Contains(string(dcContent), "db:") {
		t.Log("==== Verifying database isolation ====")
		dbPortProd := getPortFromEnvFile(t, prodPath, "HOP_PORT_DB")
		dbPortDev := getPortFromEnvFile(t, devPath, "HOP_PORT_DB")

		if dbPortProd == dbPortDev {
			t.Error("FAIL: Database ports should be different between branches")
		} else {
			t.Logf("SUCCESS: Database isolation verified - prod:%s, dev:%s", dbPortProd, dbPortDev)
		}

		// Verify volume isolation for database
		dbVolumeProd := getVolumePathFromEnvFile(t, prodPath, "HOP_VOLUME_DB_DATA")
		dbVolumeDev := getVolumePathFromEnvFile(t, devPath, "HOP_VOLUME_DB_DATA")

		if dbVolumeProd == dbVolumeDev {
			t.Error("FAIL: Database volumes should be different between branches")
		} else {
			t.Logf("SUCCESS: Database volume isolation verified")
		}
	}

	// Phase 17: Verify Redis/broker isolation (if exists)
	if strings.Contains(string(dcContent), "redis") || strings.Contains(string(dcContent), "broker:") {
		t.Log("==== Verifying Redis/broker isolation ====")

		// Try to get broker/cache ports
		brokerPortProd := tryGetPortFromEnvFile(t, prodPath, "HOP_PORT_BROKER", "HOP_PORT_REDIS", "HOP_PORT_CACHE")
		brokerPortDev := tryGetPortFromEnvFile(t, devPath, "HOP_PORT_BROKER", "HOP_PORT_REDIS", "HOP_PORT_CACHE")

		if brokerPortProd != "" && brokerPortDev != "" {
			if brokerPortProd == brokerPortDev {
				t.Error("FAIL: Broker/cache ports should be different between branches")
			} else {
				t.Logf("SUCCESS: Broker/cache isolation verified - prod:%s, dev:%s", brokerPortProd, brokerPortDev)
			}
		}
	}

	// Phase 18: Simulate development workflow
	t.Log("==== Simulating development workflow ====")

	// Make a change in development branch (add comment to a Python file)
	findAndModifyPythonFile(t, devPath)

	// Stop development environment
	t.Log("Stopping development environment...")
	env.RunGitHop(t, devPath, "env", "stop")
	time.Sleep(3 * time.Second)

	// Phase 19: Verify production is unaffected
	t.Log("Verifying production remains accessible while development is stopped...")
	testDjangoEndpoint(t, urlProd, "production")
	t.Log("✓ Production unaffected by development stop")

	// Phase 20: Restart development environment
	t.Log("Restarting development environment...")
	env.RunGitHop(t, devPath, "env", "start")
	WaitForServiceHealthy(t, devPath, webService, 300*time.Second)

	// Verify both environments accessible
	t.Log("Verifying both environments are now accessible...")
	testDjangoEndpoint(t, urlProd, "production")
	testDjangoEndpoint(t, urlDev, "development")
	t.Log("✓ Both environments running independently")

	// Phase 21: Test concurrent operation
	t.Log("==== Testing concurrent operation ====")

	// Verify status shows both environments
	statusOutput := env.RunGitHop(t, env.HubPath, "status")
	if !strings.Contains(statusOutput, "main") || !strings.Contains(statusOutput, "development") {
		t.Errorf("Status output doesn't show both branches:\n%s", statusOutput)
	}
	t.Log("✓ Status correctly shows both environments")

	// Phase 22: Verify environment variables are distinct
	t.Log("==== Verifying environment variable isolation ====")

	prodEnvPath := filepath.Join(prodPath, ".env")
	devEnvPath := filepath.Join(devPath, ".env")

	prodEnv, _ := os.ReadFile(prodEnvPath)
	devEnv, _ := os.ReadFile(devEnvPath)

	if string(prodEnv) == string(devEnv) {
		t.Error("FAIL: .env files should be different between branches")
	} else {
		t.Log("SUCCESS: Environment variables are isolated between branches")
	}

	// Phase 23: Stop both environments
	t.Log("==== Stopping all environments ====")
	env.RunGitHop(t, prodPath, "env", "stop")
	env.RunGitHop(t, devPath, "env", "stop")

	t.Log("✓✓✓ Real-world Django multi-branch workflow completed successfully ✓✓✓")
}

// identifyWebService attempts to identify the primary web service name from docker-compose.yml
func identifyWebService(dockerComposeContent string) string {
	// Common web service names in Django projects
	webServiceNames := []string{"web", "app", "django", "backend", "gunicorn", "uwsgi"}

	lines := strings.Split(dockerComposeContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Look for service definitions (e.g., "  web:" or "  app:")
		for _, serviceName := range webServiceNames {
			if trimmed == serviceName+":" {
				return serviceName
			}
		}
	}

	return ""
}

// testDjangoEndpoint tests a Django HTTP endpoint, accepting multiple valid status codes
func testDjangoEndpoint(t *testing.T, url, environment string) {
	t.Helper()

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects, we just want to verify the server responds
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to access %s endpoint %s: %v", environment, url, err)
	}
	defer resp.Body.Close()

	// Django apps may return various status codes:
	// 200 (OK), 302 (redirect to login/admin), 404 (not found), etc.
	// As long as we get a response, the server is working
	acceptableStatuses := []int{200, 302, 301, 404, 403}
	acceptable := false
	for _, status := range acceptableStatuses {
		if resp.StatusCode == status {
			acceptable = true
			break
		}
	}

	if !acceptable {
		t.Errorf("%s endpoint returned unexpected status %d (expected one of %v)",
			environment, resp.StatusCode, acceptableStatuses)
	}

	t.Logf("%s endpoint %s is accessible (status: %d)", environment, url, resp.StatusCode)
}

// findAndModifyPythonFile finds a Python file and adds a comment to simulate development work
func findAndModifyPythonFile(t *testing.T, dir string) {
	t.Helper()

	// Look for common Django files
	pythonFiles := []string{
		"manage.py",
		"settings.py",
		filepath.Join("mysite", "settings.py"),
		filepath.Join("config", "settings.py"),
	}

	for _, filename := range pythonFiles {
		fullPath := filepath.Join(dir, filename)
		if _, err := os.Stat(fullPath); err == nil {
			// File exists, add a comment
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}

			// Add a comment at the beginning
			newContent := "# Modified for development testing\n" + string(content)
			if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
				t.Logf("Warning: Failed to modify %s: %v", filename, err)
				continue
			}

			// Commit the change
			exec.Command("git", "-C", dir, "add", filename).Run()
			exec.Command("git", "-C", dir, "commit", "-m", "Development change").Run()

			t.Logf("Modified %s to simulate development work", filename)
			return
		}
	}

	t.Log("No Python file found to modify (non-critical)")
}

// tryGetPortFromEnvFile tries multiple possible port keys and returns the first found
func tryGetPortFromEnvFile(t *testing.T, dir string, keys ...string) string {
	t.Helper()

	envPath := filepath.Join(dir, ".env")
	content, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}

	envContent := string(content)
	for _, key := range keys {
		for _, line := range strings.Split(envContent, "\n") {
			if strings.HasPrefix(line, key+"=") {
				port := strings.TrimPrefix(line, key+"=")
				port = strings.TrimSpace(port)
				if port != "" {
					return port
				}
			}
		}
	}

	return ""
}
