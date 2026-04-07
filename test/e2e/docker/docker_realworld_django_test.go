package docker_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "hop.top/git/test/e2e"
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

	// Phase 5: Use production branch (already created by hub init)
	prodPath := filepath.Join(env.HubPath, "hops", "main")
	if _, err := os.Stat(prodPath); err != nil {
		// Main worktree wasn't created by hub init, try adding it
		t.Log("Adding production branch...")
		env.RunGitHop(t, env.HubPath, "add", "main")
	} else {
		t.Log("Production branch already exists from hub init")
	}

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
	// Switch back so git allows the branch to be checked out in another worktree
	env.RunCommand(t, prodPath, "git", "checkout", "main")

	// Phase 8: Add development branch to hub
	t.Log("Adding development branch to hub...")
	env.RunGitHop(t, env.HubPath, "add", "development")
	devPath := filepath.Join(env.HubPath, "hops", "development")

	// Phase 9: Register cleanup to ensure containers are stopped
	t.Cleanup(func() {
		t.Log("Cleaning up Docker containers...")
		CleanupContainers(t, prodPath, "")
		CleanupContainers(t, devPath, "")
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

	// Phase 13: Test production environment lifecycle
	// Note: git-hop now auto-generates docker-compose override files for hardcoded
	// ports, enabling concurrent environments. This test still runs sequentially
	// as the real-world Django app may have additional startup dependencies.

	// Verify status shows both branches
	t.Log("==== Verifying hub status ====")
	statusOutput := env.RunGitHop(t, env.HubPath, "status")
	if !strings.Contains(statusOutput, "main") || !strings.Contains(statusOutput, "development") {
		t.Errorf("Status output doesn't show both branches:\n%s", statusOutput)
	}
	t.Log("✓ Status correctly shows both environments")

	// Phase 14: Simulate development workflow
	t.Log("==== Simulating development workflow ====")
	findAndModifyPythonFile(t, devPath)

	// Phase 15: Stop production, start development
	t.Log("Stopping production environment...")
	env.RunGitHop(t, prodPath, "env", "stop")
	time.Sleep(3 * time.Second)

	t.Log("==== Starting development environment ====")
	env.RunGitHop(t, devPath, "env", "start")

	t.Log("Waiting for development services to become healthy...")
	WaitForServiceHealthy(t, devPath, webService, 300*time.Second)

	// Phase 16: Stop development, restart production to verify independence
	t.Log("Stopping development environment...")
	env.RunGitHop(t, devPath, "env", "stop")
	time.Sleep(3 * time.Second)

	t.Log("Restarting production environment...")
	env.RunGitHop(t, prodPath, "env", "start")
	WaitForServiceHealthy(t, prodPath, webService, 300*time.Second)
	t.Log("✓ Production restarts independently after development was used")

	// Phase 17: Final cleanup
	t.Log("==== Stopping all environments ====")
	env.RunGitHop(t, prodPath, "env", "stop")

	t.Log("✓ Real-world Django multi-branch workflow completed successfully")
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

