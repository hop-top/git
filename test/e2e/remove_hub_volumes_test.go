package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setUpVolumeHub clones a hub with a feature branch and puts volume data
// in its volumes directory, as a database bind mount leaves it. It
// returns that data file.
func setUpVolumeHub(t *testing.T, env *TestEnv) string {
	t.Helper()
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	WriteFile(t, filepath.Join(env.SeedRepoPath, "README.md"), "# Test")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", "README.md")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "Initial")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main:feature")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature")

	data := filepath.Join(env.HubPath, "volumes", "hop_main_db", "PG_VERSION")
	if err := os.MkdirAll(filepath.Dir(data), 0o755); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, data, "16\n")
	return data
}

// Removing a hub keeps its volume data, moved aside under the data home,
// with a hint; --delete-volumes deletes it; --dry-run says which; and the
// flag is refused for anything but a hub.
func TestRemoveHubKeepsVolumeData(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	env := SetupTestEnv(t)
	data := setUpVolumeHub(t, env)
	vols := filepath.Join(env.HubPath, "volumes")

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "remove", "feature", "--delete-volumes", "--no-prompt")
	if code != 129 || !strings.Contains(stderr, "--delete-volumes applies only to removing a hub") {
		t.Fatalf("branch removal with --delete-volumes: exit %d, stderr:\n%s", code, stderr)
	}
	_, stderr, code = env.RunCommandWithExit(t, env.HubPath, env.BinPath, "remove", "--merged", "--delete-volumes")
	if code != 129 {
		t.Fatalf("--merged --delete-volumes: exit %d, stderr:\n%s", code, stderr)
	}

	out := env.RunGitHop(t, env.RootDir, "remove", env.HubPath, "--dry-run", "--no-prompt")
	if !strings.Contains(out, "Would keep volume data at "+vols) {
		t.Errorf("dry-run should say the volume data is kept:\n%s", out)
	}
	out = env.RunGitHop(t, env.RootDir, "remove", env.HubPath, "--dry-run", "--no-prompt", "--delete-volumes")
	if !strings.Contains(out, "Would delete volume data at "+vols) {
		t.Errorf("dry-run --delete-volumes should say the volume data is deleted:\n%s", out)
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatalf("dry-run touched the volume data: %v", err)
	}

	stdout, stderr, code := env.RunCommandWithExit(t, env.RootDir, env.BinPath, "remove", env.HubPath, "--no-prompt")
	if code != 0 {
		t.Fatalf("remove hub: exit %d\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(env.HubPath); !os.IsNotExist(err) {
		t.Errorf("hub directory should be gone: %v", err)
	}
	moved, _ := filepath.Glob(filepath.Join(env.DataHome, "orphaned-volumes", "*", "*", "hub-*", "volumes", "hop_main_db", "PG_VERSION"))
	if len(moved) != 1 {
		t.Fatalf("volume data should be moved aside once, found %v", moved)
	}
	if got, _ := os.ReadFile(moved[0]); string(got) != "16\n" {
		t.Errorf("moved volume data = %q", got)
	}
	if !strings.Contains(stderr, "hint: kept the volume data at:") || !strings.Contains(stderr, filepath.Dir(filepath.Dir(moved[0]))) {
		t.Errorf("expected a hint naming where the data is, got:\n%s", stderr)
	}

	env2 := SetupTestEnv(t)
	data = setUpVolumeHub(t, env2)
	out = env2.RunGitHop(t, env2.RootDir, "remove", env2.HubPath, "--no-prompt", "--delete-volumes", "--json")
	if !strings.Contains(out, `"volumes":"deleted"`) && !strings.Contains(out, `"volumes": "deleted"`) {
		t.Errorf("result should say the volumes were deleted:\n%s", out)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Errorf("--delete-volumes should delete the volume data: %v", err)
	}
	if moved, _ := filepath.Glob(filepath.Join(env2.DataHome, "orphaned-volumes", "*")); len(moved) != 0 {
		t.Errorf("--delete-volumes moves nothing aside: %v", moved)
	}
}
