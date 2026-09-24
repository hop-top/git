package hop_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// legacySparseRepo is a sparse checkout set up the way git did before
// `git sparse-checkout` existed: core.sparseCheckout=true in the shared
// .git/config, patterns in .git/info/sparse-checkout, and no
// extensions.worktreeConfig. Only a/ is checked out.
func legacySparseRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	for _, f := range []string{"a/x", "b/y"} {
		writeFile(t, filepath.Join(repo, f), f+"\n", 0o644)
	}
	mustRun(t, "git", "-C", repo, "add", "-A")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "files")
	mustRun(t, "git", "-C", repo, "config", "core.sparseCheckout", "true")
	writeFile(t, filepath.Join(repo, ".git", "info", "sparse-checkout"), "/a/\n", 0o644)
	mustRun(t, "git", "-C", repo, "read-tree", "-mu", "HEAD")
	if _, err := os.Stat(filepath.Join(repo, "b")); err == nil {
		t.Fatal("setup: b/ still checked out")
	}
	return repo
}

// The plan moves the sparse keys out of the shared config and into the
// default worktree's own, and says the hub needs per-worktree config.
func TestPlanLocalConfig_LegacySparseCheckoutIsPerWorktree(t *testing.T) {
	repo := legacySparseRepo(t)
	plan, err := hop.PlanLocalConfig(git.New(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plan.CarriedKeys(), "core.sparsecheckout") {
		t.Errorf("core.sparsecheckout carried into the hub's shared config: %v", plan.CarriedKeys())
	}
	if !slices.Contains(plan.PerWorktreeKeys(), "core.sparsecheckout") {
		t.Errorf("core.sparsecheckout not planned for the default worktree: %v", plan.PerWorktreeKeys())
	}
	if !plan.HubWorktreeConfig() {
		t.Error("HubWorktreeConfig() = false for a sparse checkout")
	}
	if plan.WorktreeConfig {
		t.Error("WorktreeConfig = true: the repository did not have the extension")
	}
}

// TestConvertBare_LegacySparseCheckout: the default worktree is as sparse
// as the repository was, and a worktree added afterwards is full, with
// no sparse setting shared through the hub's config.
func TestConvertBare_LegacySparseCheckout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	repo := legacySparseRepo(t)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	result, err := conv.ConvertToBareWorktree(repo, true, false)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	if out, err := exec.Command("git", "config", "--file", filepath.Join(repo, "config"), "--get-all", "core.sparseCheckout").Output(); err == nil {
		t.Errorf("hub's shared config holds core.sparseCheckout: %q", out)
	}

	main := filepath.Join(repo, "hops", "main")
	if got := strings.TrimSpace(gitOut(t, "-C", main, "config", "--worktree", "--type=bool", "core.sparseCheckout")); got != "true" {
		t.Errorf("hops/main core.sparseCheckout (worktree) = %q, want true", got)
	}
	if _, err := os.Stat(filepath.Join(main, "b")); err == nil {
		t.Error("hops/main/b present: sparse checkout not applied")
	}
	if _, err := os.Stat(filepath.Join(main, "a", "x")); err != nil {
		t.Errorf("hops/main/a/x missing: %v", err)
	}
	if st := gitOut(t, "-C", main, "status", "--porcelain"); st != "" {
		t.Errorf("hops/main not clean after conversion:\n%s", st)
	}
	if bare := strings.TrimSpace(gitOut(t, "-C", repo, "rev-parse", "--is-bare-repository")); bare != "true" {
		t.Errorf("hub is-bare-repository = %q, want true", bare)
	}

	feat := filepath.Join(repo, "hops", "feat")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", "-b", "feat", feat, "main")
	if bare := strings.TrimSpace(gitOut(t, "-C", feat, "rev-parse", "--is-bare-repository")); bare != "false" {
		t.Errorf("hops/feat is-bare-repository = %q, want false", bare)
	}
	if out, err := exec.Command("git", "-C", feat, "config", "--type=bool", "core.sparseCheckout").Output(); err == nil && strings.TrimSpace(string(out)) == "true" {
		t.Error("hops/feat inherited core.sparseCheckout=true")
	}
	if _, err := os.Stat(filepath.Join(feat, "b", "y")); err != nil {
		t.Errorf("hops/feat is not a full checkout: %v", err)
	}
}
