package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
)

func TestE2E_PortAndVolumeIsolation(t *testing.T) {
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

	// 3. Initialize Hub
	// Create hub dir
	os.MkdirAll(env.HubPath, 0755)

	// Clone main to data home
	mainWorktreePath := filepath.Join(env.DataHome, "local", "test-repo", "main")
	os.MkdirAll(filepath.Dir(mainWorktreePath), 0755)
	env.RunCommand(t, filepath.Dir(mainWorktreePath), "git", "clone", env.BareRepoPath, "main")

	// Create Hub Config (in hubPath)
	hubTmplContent, err := os.ReadFile("fixtures/hub_config.json.tmpl")
	if err != nil {
		t.Fatalf("Failed to read hub_config.json.tmpl: %v", err)
	}
	hubTmpl, err := template.New("hub.json").Parse(string(hubTmplContent))
	if err != nil {
		t.Fatalf("Failed to parse hub_config.json.tmpl: %v", err)
	}

	var hubJsonBuf bytes.Buffer
	hubData := struct {
		RepoURI string
	}{
		RepoURI: env.BareRepoPath,
	}
	if err := hubTmpl.Execute(&hubJsonBuf, hubData); err != nil {
		t.Fatalf("Failed to execute hub_config.json.tmpl: %v", err)
	}
	WriteFile(t, filepath.Join(env.HubPath, "hop.json"), hubJsonBuf.String())

	// Create Hopspace Config (in dataHome/local/test-repo/hop.json)
	hsTmplContent, err := os.ReadFile("fixtures/hopspace_config.json.tmpl")
	if err != nil {
		t.Fatalf("Failed to read hopspace_config.json.tmpl: %v", err)
	}
	hsTmpl, err := template.New("hopspace.json").Parse(string(hsTmplContent))
	if err != nil {
		t.Fatalf("Failed to parse hopspace_config.json.tmpl: %v", err)
	}

	var hsJsonBuf bytes.Buffer
	hsData := struct {
		RepoURI      string
		WorktreePath string
		LastSync     string
	}{
		RepoURI:      env.BareRepoPath,
		WorktreePath: mainWorktreePath,
		LastSync:     time.Now().Format(time.RFC3339),
	}
	if err := hsTmpl.Execute(&hsJsonBuf, hsData); err != nil {
		t.Fatalf("Failed to execute hopspace_config.json.tmpl: %v", err)
	}
	WriteFile(t, filepath.Join(filepath.Dir(mainWorktreePath), "hop.json"), hsJsonBuf.String())

	// 4. Add Branches
	env.RunGitHop(t, env.HubPath, "add", "branch-a")
	env.RunGitHop(t, env.HubPath, "add", "branch-b")

	// 5. Verify .env generation
	branchAPath := filepath.Join(env.DataHome, "local", "test-repo", "branch-a")
	branchBPath := filepath.Join(env.DataHome, "local", "test-repo", "branch-b")

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
	env.RunGitHop(t, filepath.Join(env.HubPath, "branch-a"), "env", "start")
	env.RunGitHop(t, filepath.Join(env.HubPath, "branch-b"), "env", "start")

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

	// Cleanup
	env.RunGitHop(t, filepath.Join(env.HubPath, "branch-a"), "env", "stop")
	env.RunGitHop(t, filepath.Join(env.HubPath, "branch-b"), "env", "stop")
}
