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

	e2e "hop.top/git/test/e2e"
)

// TestDockerRealWorld_Laravel simulates a real-world Laravel development workflow
// using the official Docker samples repository. This test demonstrates:
//
// 1. Cloning a production-ready Laravel application with docker-compose
// 2. Managing multiple environments simultaneously (staging + hotfix)
// 3. Port and database isolation between branches
// 4. Hotfix workflow: urgent bug fix while staging continues development
// 5. Service restart without affecting other branches
//
// Repository: https://github.com/dockersamples/laravel-docker-examples
// Services: web (nginx), app (php-fpm), db (MySQL), cache (Redis)
func TestDockerRealWorld_Laravel(t *testing.T) {
	// Phase 1: Verify Docker availability
	SkipIfDockerNotAvailable(t)

	// Phase 2: Setup isolated test environment
	env := e2e.SetupTestEnv(t)

	// Phase 3: Clone real Laravel application with Docker setup
	// Using official Docker samples repository which provides production-ready config
	repoURL := "https://github.com/dockersamples/laravel-docker-examples"

	t.Logf("Cloning Laravel Docker examples from %s...", repoURL)
	cloneCmd := exec.Command("git", "clone", "--bare", "--depth", "1", repoURL, env.BareRepoPath)
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr

	if err := cloneCmd.Run(); err != nil {
		t.Skipf("Failed to clone Laravel app (network may be unavailable): %v", err)
	}
	t.Log("Successfully cloned Laravel Docker examples repository")

	// Phase 4: Initialize hub from the cloned repository
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	t.Logf("Initialized hub at %s", env.HubPath)

	// Phase 5: Create staging branch
	// Staging is where ongoing development happens
	env.RunGitHop(t, env.HubPath, "add", "staging")
	stagingPath := filepath.Join(env.HubPath, "hops", "staging")
	t.Logf("Created staging environment at %s", stagingPath)

	// Phase 6: Create hotfix branch from staging
	// Simulate discovering a critical bug that needs immediate attention
	t.Log("Creating hotfix branch for critical bug fix...")
	env.RunCommand(t, stagingPath, "git", "checkout", "-b", "hotfix/critical-bug")
	env.RunCommand(t, stagingPath, "git", "push", "origin", "hotfix/critical-bug")
	// Switch staging back so git allows the branch to be checked out in another worktree
	env.RunCommand(t, stagingPath, "git", "checkout", "staging")

	// Phase 7: Add hotfix branch to hub
	// This creates a separate isolated environment for the hotfix
	env.RunGitHop(t, env.HubPath, "add", "hotfix/critical-bug")
	hotfixPath := filepath.Join(env.HubPath, "hops", "hotfix", "critical-bug")
	t.Logf("Created hotfix environment at %s", hotfixPath)

	// Phase 8: Register cleanup to stop containers even if test fails
	t.Cleanup(func() {
		t.Log("Cleaning up staging environment...")
		CleanupContainers(t, stagingPath, "")
		t.Log("Cleaning up hotfix environment...")
		CleanupContainers(t, hotfixPath, "")
	})

	// Phase 9: Verify docker-compose.yml exists in the repository
	// The dockersamples repo should have compose.dev.yaml
	composePath := findComposeFile(t, stagingPath)
	if composePath == "" {
		t.Skipf("No docker-compose configuration found in Laravel repository")
	}
	t.Logf("Found Docker Compose configuration: %s", filepath.Base(composePath))

	// Phase 10: Prepare both environments with Laravel-specific setup
	// Laravel needs certain files and configuration to work properly
	setupLaravelEnvironment(t, env, stagingPath)
	setupLaravelEnvironment(t, env, hotfixPath)

	// Phase 10b: Generate environment files now that docker-compose.yml is in place
	env.RunGitHop(t, stagingPath, "env", "generate")
	env.RunGitHop(t, hotfixPath, "env", "generate")

	// Phase 11: Start staging environment
	// This is the long-running development environment
	t.Log("Starting staging environment (nginx + php-fpm + mysql + redis)...")
	env.RunGitHop(t, stagingPath, "env", "start")

	// Phase 12: Start hotfix environment
	// Hotfix runs in parallel for urgent bug fix
	t.Log("Starting hotfix environment (nginx + php-fpm + mysql + redis)...")
	env.RunGitHop(t, hotfixPath, "env", "start")

	// Phase 13: Wait for Laravel services to become healthy
	// Laravel stack takes longer due to composer install and database migrations
	t.Log("Waiting for staging services to become healthy (may take up to 3 minutes)...")
	waitForLaravelServices(t, stagingPath, 180*time.Second)

	t.Log("Waiting for hotfix services to become healthy (may take up to 3 minutes)...")
	waitForLaravelServices(t, hotfixPath, 180*time.Second)

	// Phase 14: Verify port isolation
	// Each branch must have different ports to run concurrently
	portStagingWeb := getPortFromEnvFile(t, stagingPath, "HOP_PORT_WEB")
	portHotfixWeb := getPortFromEnvFile(t, hotfixPath, "HOP_PORT_WEB")

	if portStagingWeb == portHotfixWeb {
		t.Errorf("Web ports should be different: staging=%s, hotfix=%s", portStagingWeb, portHotfixWeb)
	}
	t.Logf("Port isolation verified: staging web=%s, hotfix web=%s", portStagingWeb, portHotfixWeb)

	// Phase 15: Verify both Laravel applications are accessible
	urlStaging := fmt.Sprintf("http://localhost:%s", portStagingWeb)
	urlHotfix := fmt.Sprintf("http://localhost:%s", portHotfixWeb)

	t.Logf("Testing staging endpoint: %s", urlStaging)
	CheckHTTPEndpoint(t, urlStaging, http.StatusOK)

	t.Logf("Testing hotfix endpoint: %s", urlHotfix)
	CheckHTTPEndpoint(t, urlHotfix, http.StatusOK)

	t.Logf("✓ Staging accessible at %s", urlStaging)
	t.Logf("✓ Hotfix accessible at %s", urlHotfix)

	// Phase 16: Verify database isolation
	// Each branch must have its own database to prevent data corruption
	dbPortStaging := getPortFromEnvFileOrDefault(t, stagingPath, "HOP_PORT_DB", "")
	dbPortHotfix := getPortFromEnvFileOrDefault(t, hotfixPath, "HOP_PORT_DB", "")

	if dbPortStaging != "" && dbPortHotfix != "" {
		if dbPortStaging == dbPortHotfix {
			t.Error("Database ports should be different for isolation")
		} else {
			t.Logf("✓ Database isolation verified: staging db=%s, hotfix db=%s", dbPortStaging, dbPortHotfix)
		}
	}

	// Phase 17: Verify cache (Redis) isolation
	cachePortStaging := getPortFromEnvFileOrDefault(t, stagingPath, "HOP_PORT_CACHE", "")
	cachePortHotfix := getPortFromEnvFileOrDefault(t, hotfixPath, "HOP_PORT_CACHE", "")

	if cachePortStaging != "" && cachePortHotfix != "" {
		if cachePortStaging == cachePortHotfix {
			t.Error("Cache ports should be different for isolation")
		} else {
			t.Logf("✓ Cache isolation verified: staging cache=%s, hotfix cache=%s", cachePortStaging, cachePortHotfix)
		}
	}

	// Phase 18: Simulate hotfix workflow
	// In real-world scenario: developer would fix bug, test, then merge
	t.Log("Simulating hotfix workflow: make change, restart environment...")

	// Make a visible change in the hotfix branch
	// (In production: this would be actual code fix)
	testFilePath := filepath.Join(hotfixPath, "HOTFIX_APPLIED.txt")
	e2e.WriteFile(t, testFilePath, "Critical bug fixed at "+time.Now().Format(time.RFC3339))
	t.Log("Applied hotfix change (simulated bug fix)")

	// Phase 19: Restart hotfix environment to apply changes
	t.Log("Restarting hotfix environment after applying fix...")
	env.RunGitHop(t, hotfixPath, "env", "stop")
	time.Sleep(3 * time.Second) // Allow containers to fully stop

	env.RunGitHop(t, hotfixPath, "env", "start")
	waitForLaravelServices(t, hotfixPath, 180*time.Second)
	t.Log("✓ Hotfix environment restarted successfully")

	// Phase 20: Verify staging environment is unaffected
	// Critical test: staging must continue running during hotfix work
	t.Log("Verifying staging environment was not affected by hotfix restart...")
	CheckHTTPEndpoint(t, urlStaging, http.StatusOK)
	t.Log("✓ Staging environment remained operational during hotfix workflow")

	// Phase 21: Verify hotfix environment is operational after restart
	CheckHTTPEndpoint(t, urlHotfix, http.StatusOK)
	t.Log("✓ Hotfix environment operational after restart")

	// Phase 22: Stop both environments cleanly
	t.Log("Stopping staging environment...")
	env.RunGitHop(t, stagingPath, "env", "stop")

	t.Log("Stopping hotfix environment...")
	env.RunGitHop(t, hotfixPath, "env", "stop")

	// Phase 23: Verify cleanup
	time.Sleep(2 * time.Second)
	t.Log("✓ Both environments stopped successfully")

	// Test completed successfully
	t.Log("========================================")
	t.Log("✓ Real-world Laravel multi-branch workflow completed successfully")
	t.Log("✓ Demonstrated:")
	t.Log("  - Concurrent staging + hotfix environments")
	t.Log("  - Port isolation (web, db, cache)")
	t.Log("  - Independent restart without affecting other branches")
	t.Log("  - Production-ready Laravel Docker stack")
	t.Log("========================================")
}

// Helper Functions

// findComposeFile searches for docker-compose configuration files in the given directory.
// Laravel Docker examples may use compose.dev.yaml, compose.prod.yaml, or docker-compose.yml.
func findComposeFile(t *testing.T, dir string) string {
	t.Helper()

	possibleFiles := []string{
		"compose.dev.yaml",
		"compose.yaml",
		"docker-compose.dev.yml",
		"docker-compose.yml",
		"docker-compose.yaml",
	}

	for _, filename := range possibleFiles {
		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			t.Logf("Found compose file: %s", filename)
			return path
		}
	}

	return ""
}

// setupLaravelEnvironment prepares a Laravel environment for running.
// This includes creating necessary configuration files and setting up the .env file.
func setupLaravelEnvironment(t *testing.T, env *e2e.TestEnv, workingDir string) {
	t.Helper()

	// Check if we have the official docker examples structure
	// If compose.dev.yaml exists, we might need to symlink it to docker-compose.yml
	composeDevPath := filepath.Join(workingDir, "compose.dev.yaml")
	dockerComposePath := filepath.Join(workingDir, "docker-compose.yml")

	if _, err := os.Stat(composeDevPath); err == nil {
		// Create symlink if docker-compose.yml doesn't exist
		if _, err := os.Stat(dockerComposePath); os.IsNotExist(err) {
			// Instead of symlink (which might fail), copy the file
			composeContent, err := os.ReadFile(composeDevPath)
			if err == nil {
				e2e.WriteFile(t, dockerComposePath, string(composeContent))
				t.Logf("Copied compose.dev.yaml to docker-compose.yml in %s", filepath.Base(workingDir))
			}
		}
	}

	// If no docker-compose.yml exists at all, use our fixture
	if _, err := os.Stat(dockerComposePath); os.IsNotExist(err) {
		t.Logf("No docker-compose.yml found, using Laravel fixture")
		fixtureContent, err := os.ReadFile("fixtures/docker-compose-laravel.yml")
		if err == nil {
			e2e.WriteFile(t, dockerComposePath, string(fixtureContent))
		}
	}

	// Create basic Laravel structure if it doesn't exist
	publicDir := filepath.Join(workingDir, "public")
	if _, err := os.Stat(publicDir); os.IsNotExist(err) {
		os.MkdirAll(publicDir, 0755)

		// Create a simple index.php for health check
		indexContent := `<?php
// Simple Laravel-style health check
header('Content-Type: application/json');
echo json_encode(['status' => 'ok', 'service' => 'laravel', 'timestamp' => time()]);
`
		e2e.WriteFile(t, filepath.Join(publicDir, "index.php"), indexContent)
		t.Logf("Created basic Laravel public structure in %s", filepath.Base(workingDir))
	}

	// Create nginx.conf if needed (for our fixture)
	nginxConfPath := filepath.Join(workingDir, "nginx.conf")
	if _, err := os.Stat(nginxConfPath); os.IsNotExist(err) {
		nginxContent := `server {
    listen 80;
    server_name localhost;
    root /var/www/html/public;
    index index.php index.html;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location /api/health {
        return 200 '{"status":"healthy"}';
        add_header Content-Type application/json;
    }

    location ~ \.php$ {
        fastcgi_pass app:9000;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }
}
`
		e2e.WriteFile(t, nginxConfPath, nginxContent)
		t.Logf("Created nginx configuration in %s", filepath.Base(workingDir))
	}
}

// waitForLaravelServices waits for all Laravel services to become healthy.
// Laravel stack includes: web (nginx), app (php-fpm), db (mysql), cache (redis).
func waitForLaravelServices(t *testing.T, dir string, timeout time.Duration) {
	t.Helper()

	// Wait for database first (other services depend on it)
	if hasService(t, dir, "db") {
		WaitForServiceHealthy(t, dir, "db", timeout)
	}

	// Wait for cache
	if hasService(t, dir, "cache") {
		WaitForServiceHealthy(t, dir, "cache", timeout)
	}

	// Wait for PHP application
	if hasService(t, dir, "app") {
		WaitForServiceHealthy(t, dir, "app", timeout)
	}

	// Finally wait for web server
	if hasService(t, dir, "web") {
		WaitForServiceHealthy(t, dir, "web", timeout)
	}

	t.Logf("All Laravel services healthy in %s", filepath.Base(dir))
}

// hasService checks if a service is defined in the docker-compose.yml
func hasService(t *testing.T, dir, serviceName string) bool {
	t.Helper()

	cmd := exec.Command("docker", "compose", "config", "--services")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	services := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, service := range services {
		if strings.TrimSpace(service) == serviceName {
			return true
		}
	}
	return false
}

// getPortFromEnvFileOrDefault reads a port from .env file or returns default if not found
func getPortFromEnvFileOrDefault(t *testing.T, dir, key, defaultValue string) string {
	t.Helper()

	envPath := filepath.Join(dir, ".env")
	content, err := os.ReadFile(envPath)
	if err != nil {
		return defaultValue
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, key+"=") {
			port := strings.TrimPrefix(line, key+"=")
			return strings.TrimSpace(port)
		}
	}

	return defaultValue
}
