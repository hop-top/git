package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var restoreHintRe = regexp.MustCompile(`(?m)^hint:\s+git hop init --restore (\S+) --force$`)

// convertNoRemoteKeepingBackup converts a repository that has no remote,
// keeping the backup under a configured hop.backup.path, and returns the
// repo path and the backup path the init output tells the user to
// restore from.
func convertNoRemoteKeepingBackup(t *testing.T, env *TestEnv) (repoPath, backupPath string) {
	t.Helper()
	backupRoot := filepath.Join(env.RootDir, "bk")
	env.RunCommand(t, env.RootDir, "git", "config", "--global", "hop.backup.path", backupRoot)
	repoPath = seedPlainRepo(t, env, "proj", "main")

	_, stderr, code := env.RunCommandWithExit(t, repoPath, env.BinPath, "init", "--no-prompt", "--keep-backup", "--no-hooks")
	if code != 0 {
		t.Fatalf("init exited %d:\n%s", code, stderr)
	}
	m := restoreHintRe.FindStringSubmatch(stderr)
	if m == nil {
		t.Fatalf("init kept a backup but printed no restore hint:\n%s", stderr)
	}
	backupPath = m[1]
	if !strings.HasPrefix(backupPath, backupRoot+string(filepath.Separator)) {
		t.Fatalf("backup %s not under hop.backup.path %s", backupPath, backupRoot)
	}
	return repoPath, backupPath
}

func assertRestoredStandardRepo(t *testing.T, repoPath string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(repoPath, ".git"))
	if err != nil || !info.IsDir() {
		t.Fatalf("%s/.git is not a directory after restore: %v", repoPath, err)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "README.md")); err != nil {
		t.Errorf("README.md missing after restore: %v", err)
	}
	for _, gone := range []string{"hop.json", "hops"} {
		if _, err := os.Stat(filepath.Join(repoPath, gone)); !os.IsNotExist(err) {
			t.Errorf("%s of the converted hub survived the restore", gone)
		}
	}
}

// The converted hub occupies the original location, so a restore run
// from inside it is refused with a hint; with --force it replaces the hub
// at the recorded location, not the worktree it was run from.
func TestInitRestore_NoRemoteRefusesThenForceRestoresOriginal(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath, backupPath := convertNoRemoteKeepingBackup(t, env)
	worktree := filepath.Join(repoPath, "hops", "main")

	_, stderr, code := env.RunCommandWithExit(t, worktree, env.BinPath, "init", "--restore", backupPath)
	if code == 0 {
		t.Fatalf("restore over the converted hub succeeded without --force:\n%s", stderr)
	}
	if !strings.Contains(stderr, "not empty") || !strings.Contains(stderr, "git hop init --restore "+backupPath+" --force") {
		t.Errorf("refusal does not explain itself or hint at --force:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "hop.json")); err != nil {
		t.Fatalf("refused restore touched the hub: %v", err)
	}

	_, stderr, code = env.RunCommandWithExit(t, worktree, env.BinPath, "init", "--restore", backupPath, "--force")
	if code != 0 {
		t.Fatalf("restore --force exited %d:\n%s", code, stderr)
	}
	assertRestoredStandardRepo(t, repoPath)
}

// With the original location gone, restore needs no --force and puts the
// repository back there, leaving the directory it was run from alone.
func TestInitRestore_RestoresToRecordedLocationNotCwd(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	repoPath, backupPath := convertNoRemoteKeepingBackup(t, env)
	if err := os.RemoveAll(repoPath); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(env.RootDir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0755); err != nil {
		t.Fatal(err)
	}

	out, stderr, code := env.RunCommandWithExit(t, elsewhere, env.BinPath, "init", "--restore", backupPath)
	if code != 0 {
		t.Fatalf("restore exited %d:\n%s", code, stderr)
	}
	assertRestoredStandardRepo(t, repoPath)
	m := regexp.MustCompile(`(?m)^Repository restored to: (.+)$`).FindStringSubmatch(out)
	if m == nil || !samePath(t, m[1], repoPath) {
		t.Errorf("restore does not report the recorded location %s:\n%s", repoPath, out)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("restore wrote into the cwd %s", elsewhere)
	}
}
