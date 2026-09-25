package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// dryRunTree records every path under root, relative, with its kind,
// permission bits and content hash (or link target). Everything a run
// can write lives under the test's RootDir: the repositories, HOME and
// its rc files, the XDG dirs (registry, state, hopspace hooks), the
// global git config and the backups.
func dryRunTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			dest, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "link:" + dest
		case d.IsDir():
			snap[rel] = fmt.Sprintf("dir %o", info.Mode().Perm())
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			snap[rel] = fmt.Sprintf("file %o %s", info.Mode().Perm(), hex.EncodeToString(sum[:]))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

// assertTreeUnchanged fails with every path a dry run created, removed
// or changed.
func assertTreeUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	var diffs []string
	for p, v := range before {
		switch w, ok := after[p]; {
		case !ok:
			diffs = append(diffs, "removed  "+p)
		case w != v:
			diffs = append(diffs, "changed  "+p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			diffs = append(diffs, "created  "+p)
		}
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		t.Errorf("dry run changed %d path(s):\n  %s", len(diffs), strings.Join(diffs, "\n  "))
	}
}

// runInit runs the binary with stdin fed from input.
func runInit(t *testing.T, env *TestEnv, dir, input string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(env.BinPath, args...)
	cmd.Dir = dir
	cmd.Env = env.EnvVars
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	t.Logf("Running: git-hop %v in %s", args, dir)
	code := 0
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run git-hop: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// seedRepoWithHooks is a standard repository with an origin and a
// committed executable hook: a real run would mirror the hook into the
// hopspace and register the repository under that origin's org/repo.
func seedRepoWithHooks(t *testing.T, env *TestEnv, name string) string {
	t.Helper()
	repoPath := seedPlainRepo(t, env, name, "main")
	hook := filepath.Join(repoPath, ".git-hop", "hooks", "post-worktree-add")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, hook, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	env.RunCommand(t, repoPath, "git", "add", ".git-hop")
	env.RunCommand(t, repoPath, "git", "commit", "-q", "-m", "hooks")
	env.RunCommand(t, repoPath, "git", "remote", "add", "origin", "https://github.com/acme/"+name+".git")
	return repoPath
}

// dryRunCase sets up a fixture and returns where to run init, its stdin
// and its arguments. Every case passes -n.
type dryRunCase struct {
	name  string
	setup func(t *testing.T, env *TestEnv) (dir, input string, args []string)
}

// writeFlags ask for every optional write init can make: the hooks
// directory, the hook mirror (copy mode) and the shell integration.
var writeFlags = []string{"--hooks", "copy", "--enable-chdir"}

func initArgs(extra ...string) []string {
	return append(append([]string{"init", "-n"}, extra...), writeFlags...)
}

var dryRunCases = []dryRunCase{
	{"convert bare", func(t *testing.T, env *TestEnv) (string, string, []string) {
		return seedRepoWithHooks(t, env, "bare"), "", initArgs("--no-prompt")
	}},
	{"convert regular", func(t *testing.T, env *TestEnv) (string, string, []string) {
		return seedRepoWithHooks(t, env, "regular"), "", initArgs("--no-prompt", "--regular")
	}},
	{"convert from the menu", func(t *testing.T, env *TestEnv) (string, string, []string) {
		return seedRepoWithHooks(t, env, "menu"), "1\n", initArgs()
	}},
	{"convert with stale index stat data", func(t *testing.T, env *TestEnv) (string, string, []string) {
		// A plain `git status` refreshes the index here, rewriting it.
		repo := seedRepoWithHooks(t, env, "stale")
		old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(filepath.Join(repo, "README.md"), old, old); err != nil {
			t.Fatal(err)
		}
		return repo, "", initArgs("--no-prompt")
	}},
	{"register as-is", func(t *testing.T, env *TestEnv) (string, string, []string) {
		return seedRepoWithHooks(t, env, "asis"), "3\n", initArgs()
	}},
	{"linked worktree of a standard repo", func(t *testing.T, env *TestEnv) (string, string, []string) {
		repo := seedRepoWithHooks(t, env, "linked")
		wt := filepath.Join(env.RootDir, "linked-feat")
		env.RunCommand(t, repo, "git", "worktree", "add", "-q", "-b", "feat", wt)
		return wt, "", initArgs("--no-prompt", "--regular")
	}},
	{"backfill a bare hub without hop.json", func(t *testing.T, env *TestEnv) (string, string, []string) {
		src := seedRepoWithHooks(t, env, "adopt-src")
		hub := filepath.Join(env.RootDir, "adopt")
		env.RunCommand(t, env.RootDir, "git", "clone", "-q", "--bare", src, filepath.Join(hub, ".git"))
		env.RunCommand(t, hub, "git", "config", "core.bare", "true")
		env.RunCommand(t, hub, "git", "worktree", "add", "-q", filepath.Join(hub, "hops", "main"), "main")
		return hub, "", initArgs()
	}},
	{"already initialized hub", func(t *testing.T, env *TestEnv) (string, string, []string) {
		repo := seedRepoWithHooks(t, env, "hub")
		env.RunCommand(t, repo, env.BinPath, "init", "--no-prompt", "--no-hooks")
		return repo, "", initArgs()
	}},
	{"linked worktree of a hub", func(t *testing.T, env *TestEnv) (string, string, []string) {
		repo := seedRepoWithHooks(t, env, "hubwt")
		env.RunCommand(t, repo, env.BinPath, "init", "--no-prompt", "--no-hooks")
		return filepath.Join(repo, "hops", "main"), "", initArgs()
	}},
	{"restore over the hub with --force", func(t *testing.T, env *TestEnv) (string, string, []string) {
		_, backup := convertNoRemoteKeepingBackup(t, env)
		return env.RootDir, "", []string{"init", "-n", "--restore", backup, "--force"}
	}},
	{"restore to a missing location", func(t *testing.T, env *TestEnv) (string, string, []string) {
		repo, backup := convertNoRemoteKeepingBackup(t, env)
		if err := os.RemoveAll(repo); err != nil {
			t.Fatal(err)
		}
		return env.RootDir, "", []string{"init", "-n", "--restore", backup}
	}},
}

// -n never changes anything, whatever init would do for real: the
// directory tree, git config, registry and state, hopspace, rc files and
// backups are byte-identical before and after, in every init mode.
func TestInitDryRun_ChangesNothingInAnyMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	for _, tc := range dryRunCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := SetupTestEnv(t)
			env.EnvVars = append(env.EnvVars, "SHELL=/bin/bash")
			WriteFile(t, filepath.Join(env.RootDir, ".bashrc"), "# rc\n")
			dir, input, args := tc.setup(t, env)

			before := dryRunTree(t, env.RootDir)
			stdout, stderr, code := runInit(t, env, dir, input, args...)
			if code != 0 {
				t.Fatalf("init %v exited %d:\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
			}
			if !strings.Contains(stdout, "DRY RUN - No changes will be made") {
				t.Errorf("no dry-run banner:\n%s", stdout)
			}
			assertTreeUnchanged(t, before, dryRunTree(t, env.RootDir))
		})
	}
}

var preRestoreAsideRe = regexp.MustCompile(`(?m)^Would move (.+) aside to (.+\.pre-restore-\d{8}T\d{6}Z)$`)

// restore -n over an occupied location, with --force, names the target,
// says it is occupied and where it would move, and moves nothing.
func TestInitRestoreDryRun_OccupiedWithForcePreviews(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	_, backupPath := convertNoRemoteKeepingBackup(t, env)
	repoPath := recordedTarget(t, backupPath)
	before := dryRunTree(t, env.RootDir)

	stdout, stderr, code := env.RunCommandWithExit(t, env.RootDir, env.BinPath, "init", "--restore", backupPath, "--force", "-n")
	if code != 0 {
		t.Fatalf("restore -n exited %d:\n%s", code, stderr)
	}
	assertTreeUnchanged(t, before, dryRunTree(t, env.RootDir))

	for _, want := range []string{
		"DRY RUN - No changes will be made",
		"Backup: " + backupPath,
		"Target: " + repoPath + " (recorded in backup-info.json)",
		"Target is occupied",
		"Would restore " + filepath.Join(backupPath, "original") + " to " + repoPath,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("preview lacks %q:\n%s", want, stdout)
		}
	}
	m := preRestoreAsideRe.FindStringSubmatch(stdout)
	if m == nil || m[1] != repoPath || !strings.HasPrefix(m[2], repoPath+".pre-restore-") {
		t.Errorf("preview does not say where %s would move:\n%s", repoPath, stdout)
	}
	for _, gone := range []string{"Restore successful", "Restored from backup", "moved aside to"} {
		if strings.Contains(stdout, gone) {
			t.Errorf("preview reports a restore that must not have happened (%q):\n%s", gone, stdout)
		}
	}
	if !strings.Contains(stderr, "git hop init --restore "+backupPath+" --force\n") {
		t.Errorf("preview does not hint at the command that restores:\n%s", stderr)
	}
}

// restore -n to a missing location says so and that nothing would move.
func TestInitRestoreDryRun_UnoccupiedPreviews(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	_, backupPath := convertNoRemoteKeepingBackup(t, env)
	repoPath := recordedTarget(t, backupPath)
	if err := os.RemoveAll(repoPath); err != nil {
		t.Fatal(err)
	}
	before := dryRunTree(t, env.RootDir)

	stdout, stderr, code := env.RunCommandWithExit(t, env.RootDir, env.BinPath, "init", "--restore", backupPath, "-n")
	if code != 0 {
		t.Fatalf("restore -n exited %d:\n%s", code, stderr)
	}
	assertTreeUnchanged(t, before, dryRunTree(t, env.RootDir))
	if _, err := os.Lstat(repoPath); !os.IsNotExist(err) {
		t.Fatalf("restore -n created %s", repoPath)
	}
	for _, want := range []string{
		"Target: " + repoPath + " (recorded in backup-info.json)",
		"Target does not exist; nothing would be moved aside",
		"Would restore " + filepath.Join(backupPath, "original") + " to " + repoPath,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("preview lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "aside to") {
		t.Errorf("preview moves something aside that is not there:\n%s", stdout)
	}
	if !strings.Contains(stderr, "git hop init --restore "+backupPath+"\n") {
		t.Errorf("preview does not hint at the command that restores:\n%s", stderr)
	}
}

// refusalLines is the error:, fatal: and hint: lines of stderr: what a
// refusal says and advises, without the progress lines around it.
func refusalLines(stderr string) []string {
	var out []string
	for _, l := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(l, "error:") || strings.HasPrefix(l, "fatal:") || strings.HasPrefix(l, "hint:") {
			out = append(out, l)
		}
	}
	return out
}

// recordedTarget is the original location backup-info.json records: the
// path restore names, as init resolved it when it took the backup.
func recordedTarget(t *testing.T, backupPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(backupPath, "backup-info.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		OriginalPath string `json:"originalPath"`
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.OriginalPath == "" {
		t.Fatalf("backup-info.json records no originalPath (%v):\n%s", err, data)
	}
	return meta.OriginalPath
}

// setRecordedTarget rewrites the original location backup-info.json
// records.
func setRecordedTarget(t *testing.T, backupPath, target string) {
	t.Helper()
	p := filepath.Join(backupPath, "backup-info.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	meta["originalPath"] = target
	out, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	WriteFile(t, p, string(out))
}

// A dry run refuses what a real restore refuses, with the same error and
// exit code, and changes nothing either way.
func TestInitRestoreDryRun_RefusesAsARealRunDoes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, env *TestEnv, repoPath, backupPath string) []string
		want  string
	}{
		{"occupied without --force", func(t *testing.T, env *TestEnv, repoPath, backupPath string) []string {
			return []string{"init", "--restore", backupPath}
		}, "not empty"},
		{"no recorded location", func(t *testing.T, env *TestEnv, repoPath, backupPath string) []string {
			setRecordedTarget(t, backupPath, "")
			return []string{"init", "--restore", backupPath, "--force"}
		}, "records no original location"},
		{"backup inside the target", func(t *testing.T, env *TestEnv, repoPath, backupPath string) []string {
			setRecordedTarget(t, backupPath, filepath.Join(env.RootDir, "bk"))
			return []string{"init", "--restore", backupPath, "--force"}
		}, "lies inside"},
		{"backup without its copy", func(t *testing.T, env *TestEnv, repoPath, backupPath string) []string {
			if err := os.Rename(filepath.Join(backupPath, "original"), filepath.Join(backupPath, "moved")); err != nil {
				t.Fatal(err)
			}
			return []string{"init", "--restore", backupPath, "--force"}
		}, "backup not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := SetupTestEnv(t)
			repoPath, backupPath := convertNoRemoteKeepingBackup(t, env)
			args := tc.setup(t, env, repoPath, backupPath)
			before := dryRunTree(t, env.RootDir)

			_, dryErr, dryCode := env.RunCommandWithExit(t, env.RootDir, env.BinPath, append(args, "-n")...)
			assertTreeUnchanged(t, before, dryRunTree(t, env.RootDir))
			_, realErr, realCode := env.RunCommandWithExit(t, env.RootDir, env.BinPath, args...)
			assertTreeUnchanged(t, before, dryRunTree(t, env.RootDir))

			if realCode == 0 || dryCode != realCode {
				t.Errorf("exit codes: dry run %d, real run %d (want equal, non-zero)", dryCode, realCode)
			}
			dryLines, realLines := refusalLines(dryErr), refusalLines(realErr)
			if strings.Join(dryLines, "\n") != strings.Join(realLines, "\n") {
				t.Errorf("dry run refuses differently:\n dry: %q\nreal: %q", dryLines, realLines)
			}
			if !strings.Contains(realErr, tc.want) {
				t.Errorf("refusal lacks %q:\n%s", tc.want, realErr)
			}
		})
	}
}
