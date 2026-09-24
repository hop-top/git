package hop_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// A bare conversion of a dirty repo is refused by the clean check unless
// the converter is forced; Force is what `git hop init --force` sets.
func TestConvertToBareWorktree_ForceBypassesCleanCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}

	repoPath := initRepoWithDotGithub(t)
	untracked := filepath.Join(repoPath, "wip.txt")
	if err := os.WriteFile(untracked, []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.Force = true

	result, err := conv.ConvertToBareWorktree(repoPath, true, true)
	if err != nil {
		t.Fatalf("forced bare conversion of a dirty repo failed: %v (errors=%v)", err, result.Errors)
	}
	if !result.Success {
		t.Fatalf("forced conversion not marked success; errors=%v", result.Errors)
	}
	// The uncommitted file is carried into the new worktree.
	if _, err := os.Stat(filepath.Join(repoPath, "hops", "main", "wip.txt")); err != nil {
		t.Errorf("uncommitted file not carried into hops/main: %v", err)
	}
}

// Without Force the clean check still refuses a dirty bare conversion.
// Guards the inverse direction: the fix must not drop the check.
func TestConvertToBareWorktree_DirtyRefusedWithoutForce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}

	repoPath := initRepoWithDotGithub(t)
	if err := os.WriteFile(filepath.Join(repoPath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	if _, err := conv.ConvertToBareWorktree(repoPath, true, true); err == nil {
		t.Fatal("dirty bare conversion succeeded without Force; want the clean check to refuse")
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		t.Errorf("refused conversion touched the repo: %v", err)
	}
}
