package hop_test

// T-0166 regressions: two bugs surfaced when converting a repo with
// pre-existing subdirs (like .github/) and exec-bit-tagged scripts.
//
// 1. moveFilesToWorktree collides on subdir names that `git worktree add`
//    pre-populates from the bare repo's refs.
// 2. BackupManager.copyFile / copyDir strips mode bits — restore-on-failure
//    drops exec bits on .sh files.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// initRepoWithDotGithub builds a real git repo with .github/workflows/ci.yml
// and an executable script — the canonical T-0166 repro shape.
func initRepoWithDotGithub(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "git-hop-t0166-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	repoPath := filepath.Join(tmpDir, "test-repo")

	mustRun(t, "git", "init", repoPath)
	mustRun(t, "git", "-C", repoPath, "config", "user.name", "T")
	mustRun(t, "git", "-C", repoPath, "config", "user.email", "t@e.x")

	if err := os.MkdirAll(filepath.Join(repoPath, ".github", "workflows"), 0755); err != nil {
		t.Fatalf("mkdir .github: %v", err)
	}
	wf := filepath.Join(repoPath, ".github", "workflows", "ci.yml")
	if err := os.WriteFile(wf, []byte("name: ci\n"), 0644); err != nil {
		t.Fatalf("write ci.yml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoPath, "scripts"), 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	sh := filepath.Join(repoPath, "scripts", "build.sh")
	if err := os.WriteFile(sh, []byte("#!/bin/sh\necho ok\n"), 0755); err != nil {
		t.Fatalf("write build.sh: %v", err)
	}
	rdme := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(rdme, []byte("# t\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	mustRun(t, "git", "-C", repoPath, "add", "-A")
	mustRun(t, "git", "-C", repoPath, "commit", "-m", "init")
	mustRun(t, "git", "-C", repoPath, "branch", "-M", "main")

	return repoPath
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// TestConvertToBareWorktree_PreservesDotGithub reproduces bug 1: source repo
// has .github/ directory; bare conversion must succeed without rename collision.
func TestConvertToBareWorktree_PreservesDotGithub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}

	repoPath := initRepoWithDotGithub(t)

	fs := afero.NewOsFs()
	g := git.New()
	conv := hop.NewConverter(fs, g)
	conv.KeepBackup = false

	result, err := conv.ConvertToBareWorktree(repoPath, true, false)
	if err != nil {
		for _, e := range result.Errors {
			t.Logf("error: %s", e)
		}
		t.Fatalf("conversion failed with .github present: %v", err)
	}
	if !result.Success {
		t.Fatalf("conversion not marked success; errors=%v", result.Errors)
	}

	// Bare conversion places main worktree under worktrees/main per
	// performConversion's mainPath. .github/ + scripts/ must survive.
	mainWT := filepath.Join(repoPath, "worktrees", "main")
	for _, want := range []string{
		filepath.Join(mainWT, ".github", "workflows", "ci.yml"),
		filepath.Join(mainWT, "scripts", "build.sh"),
		filepath.Join(mainWT, "README.md"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to exist post-conversion: %v", want, err)
		}
	}
	// Exec bits on tracked scripts must survive the worktree-add-then-merge
	// path (defense in depth — conversion uses git checkout, but ensures the
	// merge logic doesn't mangle modes either).
	if info, err := os.Stat(filepath.Join(mainWT, "scripts", "build.sh")); err == nil {
		if info.Mode().Perm()&0111 == 0 {
			t.Errorf("build.sh exec bit dropped during conversion: %v", info.Mode())
		}
	}
}

// TestBackupRestore_PreservesExecBits reproduces bug 2: backup + restore must
// preserve the exec bits of executable files (e.g., .sh scripts).
func TestBackupRestore_PreservesExecBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}

	repoPath := initRepoWithDotGithub(t)

	// stat exec bits before
	before, err := os.Stat(filepath.Join(repoPath, "scripts", "build.sh"))
	if err != nil {
		t.Fatalf("stat build.sh pre: %v", err)
	}
	if before.Mode().Perm()&0111 == 0 {
		t.Fatalf("precondition: build.sh expected exec bit, got %v", before.Mode())
	}

	fs := afero.NewOsFs()
	g := git.New()
	bm, err := hop.NewBackupManager(fs, g, "t0166", "exec-bits")
	if err != nil {
		t.Fatalf("NewBackupManager: %v", err)
	}
	if err := bm.CreateBackup(repoPath); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	t.Cleanup(func() { _ = bm.Cleanup() })

	// blow away source
	if err := os.RemoveAll(repoPath); err != nil {
		t.Fatalf("rm source: %v", err)
	}

	if err := bm.Restore(repoPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	after, err := os.Stat(filepath.Join(repoPath, "scripts", "build.sh"))
	if err != nil {
		t.Fatalf("stat build.sh post: %v", err)
	}
	if after.Mode().Perm()&0111 == 0 {
		t.Errorf("exec bit dropped after restore: before=%v after=%v",
			before.Mode(), after.Mode())
	}
}

// TestConvertFailureRestore_PreservesExecBits — happy-path conversion succeeds
// post-fix; cataloging mode parity across backup/restore round-trip is the
// regression guard. This builds a small ToC of executable files and asserts
// every one survives a backup → restore cycle.
func TestConvertFailureRestore_PreservesExecBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}

	repoPath := initRepoWithDotGithub(t)

	// build catalog of executable files
	type entry struct {
		path string
		mode os.FileMode
	}
	var catalog []entry
	if err := filepath.Walk(repoPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode().Perm()&0111 != 0 {
			rel, _ := filepath.Rel(repoPath, p)
			catalog = append(catalog, entry{rel, info.Mode().Perm()})
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("catalog empty — fixture should include exec files")
	}

	fs := afero.NewOsFs()
	g := git.New()
	bm, err := hop.NewBackupManager(fs, g, "t0166", "catalog")
	if err != nil {
		t.Fatalf("NewBackupManager: %v", err)
	}
	if err := bm.CreateBackup(repoPath); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	t.Cleanup(func() { _ = bm.Cleanup() })

	if err := os.RemoveAll(repoPath); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := bm.Restore(repoPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for _, e := range catalog {
		info, err := os.Stat(filepath.Join(repoPath, e.path))
		if err != nil {
			t.Errorf("%s missing post-restore: %v", e.path, err)
			continue
		}
		if info.Mode().Perm() != e.mode {
			t.Errorf("%s mode drift: want %v got %v", e.path, e.mode, info.Mode().Perm())
		}
	}
}
