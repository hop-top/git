package docker_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	e2e "hop.top/git/test/e2e"
)

// TestDockerRealWorld_NextJS tests git-hop with a real Next.js application from the official Next.js repository.
// This test demonstrates a real-world maintainer workflow:
// - Clones actual Next.js docker-compose example from GitHub
// - Creates multiple branches with full Docker isolation
// - Runs Next.js apps simultaneously on different ports
// - Simulates development workflow (stop, modify, restart)
// - Verifies services are independently accessible
func TestDockerRealWorld_NextJS(t *testing.T) {
	// Phase 1: Check prerequisites
	SkipIfDockerNotAvailable(t)

	// Phase 2: Setup isolated test environment
	env := e2e.SetupTestEnv(t)

	// Phase 3: Clone real Next.js docker-compose example from GitHub
	// Using a shallow clone with limited depth to speed up the test
	repoURL := "https://github.com/vercel/next.js"
	t.Logf("Cloning Next.js repository from GitHub...")

	// Clone to a temporary directory first
	tempClonePath := filepath.Join(env.RootDir, "nextjs-temp")
	cloneCmd := exec.Command("git", "clone",
		"--depth", "1",
		"--filter=blob:none",
		"--sparse",
		repoURL,
		tempClonePath)
	cloneCmd.Env = env.EnvVars

	if output, err := cloneCmd.CombinedOutput(); err != nil {
		t.Skipf("Failed to clone Next.js repository: %v\nOutput: %s\n(This might be a network issue or rate limit)", err, string(output))
	}

	// Phase 4: Checkout only the docker-compose example
	t.Log("Checking out docker-compose example...")
	env.RunCommand(t, tempClonePath, "git", "sparse-checkout", "set", "examples/with-docker-compose")

	// Phase 5: Copy the example to seed repository and prepare bare repo
	examplePath := filepath.Join(tempClonePath, "examples", "with-docker-compose")

	// Verify the example exists and has docker compose files
	if _, err := os.Stat(examplePath); err != nil {
		t.Fatalf("Docker compose example not found at %s: %v", examplePath, err)
	}

	// Check for compose files (Next.js uses compose.dev.yaml)
	composeDevFile := filepath.Join(examplePath, "next-app", "compose.dev.yaml")
	if _, err := os.Stat(composeDevFile); err != nil {
		t.Skipf("compose.dev.yaml not found at %s: %v (example structure may have changed)", composeDevFile, err)
	}

	// Initialize bare repository
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)

	// Clone bare repo to seed location
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)

	// Copy Next.js example files to seed repository
	// We'll work with the next-app subdirectory which contains the actual app
	nextAppSrc := filepath.Join(examplePath, "next-app")
	copyDir(t, nextAppSrc, env.SeedRepoPath)

	// Phase 6: Rename compose.dev.yaml to docker-compose.yml for git-hop compatibility
	composeDevPath := filepath.Join(env.SeedRepoPath, "compose.dev.yaml")
	dockerComposePath := filepath.Join(env.SeedRepoPath, "docker-compose.yml")
	if err := os.Rename(composeDevPath, dockerComposePath); err != nil {
		t.Fatalf("Failed to rename compose file: %v", err)
	}

	// Phase 7: Commit and push to bare repository
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add Next.js docker-compose example")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	// Phase 8: Create a feature branch to simulate maintainer workflow
	env.RunCommand(t, env.SeedRepoPath, "git", "checkout", "-b", "feature/new-page")

	// Make a visible change to the feature branch
	readmePath := filepath.Join(env.SeedRepoPath, "README.md")
	readmeContent, _ := os.ReadFile(readmePath)
	e2e.WriteFile(t, readmePath, string(readmeContent)+"\n\n## Feature Branch\nThis is the feature branch with new changes.\n")

	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Add feature branch changes")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "feature/new-page")

	// Phase 9: Initialize git-hop hub
	t.Log("Initializing git-hop hub...")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	// Phase 10: Add main branch
	t.Log("Adding main branch...")
	env.RunGitHop(t, env.HubPath, "add", "main")
	mainPath := filepath.Join(env.HubPath, "hops", "main")

	// Phase 11: Add feature branch
	t.Log("Adding feature branch...")
	env.RunGitHop(t, env.HubPath, "add", "feature/new-page")
	featurePath := filepath.Join(env.HubPath, "hops", "feature", "new-page")

	// Phase 12: Register cleanup for both branches
	t.Cleanup(func() {
		t.Log("Cleaning up main branch containers...")
		CleanupContainers(t, mainPath, "main")
		t.Log("Cleaning up feature branch containers...")
		CleanupContainers(t, featurePath, "feature/new-page")
	})

	// Phase 13: Verify docker-compose.yml exists in both worktrees
	for _, path := range []string{mainPath, featurePath} {
		dcPath := filepath.Join(path, "docker-compose.yml")
		if _, err := os.Stat(dcPath); err != nil {
			t.Fatalf("docker-compose.yml not found at %s: %v", dcPath, err)
		}
	}

	// Phase 14: Read and log .env files to verify port allocation
	envContentMain, _ := os.ReadFile(filepath.Join(mainPath, ".env"))
	envContentFeature, _ := os.ReadFile(filepath.Join(featurePath, ".env"))

	t.Logf("Main branch .env:\n%s", string(envContentMain))
	t.Logf("Feature branch .env:\n%s", string(envContentFeature))

	// Phase 15: Start main branch services
	t.Log("Starting main branch services...")
	env.RunGitHop(t, mainPath, "env", "start")

	// Phase 16: Start feature branch services
	t.Log("Starting feature branch services...")
	env.RunGitHop(t, featurePath, "env", "start")

	// Phase 17: Wait for services to be ready (Next.js takes time to build and start)
	// Next.js services typically expose port 3000, mapped to HOP_PORT_NEXT
	t.Log("Waiting for main branch services to be ready...")
	WaitForContainerReady(t, mainPath, "main", 180*time.Second)

	t.Log("Waiting for feature branch services to be ready...")
	WaitForContainerReady(t, featurePath, "feature/new-page", 180*time.Second)

	// Give Next.js additional time to complete initial build
	t.Log("Waiting for Next.js build to complete...")
	time.Sleep(30 * time.Second)

	// Phase 18: Extract allocated ports from .env files
	portMain := getPortFromEnvFile(t, mainPath, "HOP_PORT_NEXT")
	portFeature := getPortFromEnvFile(t, featurePath, "HOP_PORT_NEXT")

	// Phase 19: Verify port isolation - each branch must have unique port
	VerifyPortIsolation(t, portMain, portFeature, "main branch", "feature branch")

	// Phase 20: Verify both Next.js applications are accessible
	urlMain := fmt.Sprintf("http://localhost:%s", portMain)
	urlFeature := fmt.Sprintf("http://localhost:%s", portFeature)

	t.Logf("Testing main branch at %s", urlMain)
	CheckHTTPEndpoint(t, urlMain, http.StatusOK)

	t.Logf("Testing feature branch at %s", urlFeature)
	CheckHTTPEndpoint(t, urlFeature, http.StatusOK)

	t.Logf("✓ Main branch Next.js app accessible at %s", urlMain)
	t.Logf("✓ Feature branch Next.js app accessible at %s", urlFeature)

	// Phase 21: Simulate real maintainer workflow - stop feature, modify, restart
	t.Log("Simulating maintainer workflow: stopping feature branch for development...")
	env.RunGitHop(t, featurePath, "env", "stop")

	// Wait for containers to stop
	time.Sleep(5 * time.Second)

	// Phase 22: Make a change to the feature branch (simulate development work)
	t.Log("Making changes to feature branch...")
	packageJsonPath := filepath.Join(featurePath, "package.json")
	packageContent, err := os.ReadFile(packageJsonPath)
	if err != nil {
		t.Logf("Warning: Could not read package.json: %v", err)
	} else {
		// Add a comment to package.json to simulate a change
		modifiedContent := "// Modified during development\n" + string(packageContent)
		e2e.WriteFile(t, packageJsonPath, modifiedContent)
		t.Log("Modified package.json in feature branch")
	}

	// Phase 23: Restart feature branch after modifications
	t.Log("Restarting feature branch after modifications...")
	env.RunGitHop(t, featurePath, "env", "start")

	// Wait for services to restart
	t.Log("Waiting for feature branch to restart...")
	WaitForContainerReady(t, featurePath, "feature/new-page", 180*time.Second)
	time.Sleep(30 * time.Second) // Wait for Next.js rebuild

	// Phase 24: Verify feature branch is still accessible after restart
	t.Logf("Verifying feature branch still accessible at %s", urlFeature)
	CheckHTTPEndpoint(t, urlFeature, http.StatusOK)
	t.Log("✓ Feature branch restarted successfully after changes")

	// Phase 25: Verify main branch is still running (unchanged during feature branch work)
	t.Logf("Verifying main branch still running at %s", urlMain)
	CheckHTTPEndpoint(t, urlMain, http.StatusOK)
	t.Log("✓ Main branch remained operational during feature branch restart")

	// Phase 26: Check status of both branches
	statusOutput := env.RunGitHop(t, env.HubPath, "status")
	t.Logf("Hub status:\n%s", statusOutput)

	// Phase 27: Stop both branches cleanly
	t.Log("Stopping main branch...")
	env.RunGitHop(t, mainPath, "env", "stop")

	t.Log("Stopping feature branch...")
	env.RunGitHop(t, featurePath, "env", "stop")

	// Phase 28: Final verification
	time.Sleep(5 * time.Second)
	finalStatus := env.RunGitHop(t, env.HubPath, "status")
	t.Logf("Final hub status:\n%s", finalStatus)

	t.Log("✓ Real-world Next.js multi-branch workflow completed successfully")
}

// copyDir recursively copies a directory
func copyDir(t *testing.T, src, dst string) {
	t.Helper()

	// Create destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("Failed to read directory %s: %v", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			copyDir(t, srcPath, dstPath)
		} else {
			content, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", srcPath, err)
			}
			if err := os.WriteFile(dstPath, content, 0644); err != nil {
				t.Fatalf("Failed to write file %s: %v", dstPath, err)
			}
		}
	}
}
