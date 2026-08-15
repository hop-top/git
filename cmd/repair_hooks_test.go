package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// Repair hooks must resolve through the shared hooks.Runner, which means
// all three levels (repo, hopspace, global) fire — not just the global
// one — and the standard GIT_HOP_* environment is supplied.

// repairHookHub builds a hub directory containing a hop.json with the
// given org/repo, plus the XDG dirs the runner consults. It points
// XDG_CONFIG_HOME and XDG_DATA_HOME at the temp tree so the global and
// hopspace hook levels are isolated from the developer's real config.
func repairHookHub(t *testing.T, org, repo string) (hubPath string, fs afero.Fs) {
	t.Helper()

	root := t.TempDir()
	hubPath = filepath.Join(root, "hub")
	if err := os.MkdirAll(hubPath, 0o755); err != nil {
		t.Fatalf("mkdir hub: %v", err)
	}

	hopJSON := `{"repo":{"uri":"https://github.com/` + org + `/` + repo +
		`","org":"` + org + `","repo":"` + repo +
		`","defaultBranch":"main"},"branches":{},"settings":{}}`
	if err := os.WriteFile(filepath.Join(hubPath, "hop.json"), []byte(hopJSON), 0o644); err != nil {
		t.Fatalf("write hop.json: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("GIT_HOP_DATA_HOME", filepath.Join(root, "data", "git-hop"))

	return hubPath, afero.NewOsFs()
}

// writeHookScript drops an executable script at path, creating parents.
func writeHookScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write hook %s: %v", path, err)
	}
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook scripts are POSIX shell")
	}
}

// A repo-level pre-repair hook lives at <hub>/.git-hop/hooks/pre-repair.
// Before repair used hooks.Runner it looked only in the global dir, so
// this hook never fired and the repair silently ran unhooked.
func TestRunRepairHook_FiresRepoLevelHook(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	marker := filepath.Join(t.TempDir(), "repo-level.fired")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\ntouch \""+marker+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("repo-level pre-repair hook did not fire: %v", err)
	}
}

// The hopspace level sits between repo and global and is keyed by the
// 3-part repo ID. It was equally unreachable before the fix.
func TestRunRepairHook_FiresHopspaceLevelHook(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	marker := filepath.Join(t.TempDir(), "hopspace-level.fired")
	hopspaceHook := filepath.Join(
		os.Getenv("GIT_HOP_DATA_HOME"),
		"github.com", "acme", "widgets", "hooks", "pre-repair",
	)
	writeHookScript(t, hopspaceHook, "#!/bin/sh\ntouch \""+marker+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hopspace-level pre-repair hook did not fire: %v", err)
	}
}

// The global level must keep working — this is the only level the old
// hand-rolled path supported, so it is the regression guard.
func TestRunRepairHook_FiresGlobalHook(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	marker := filepath.Join(t.TempDir(), "global.fired")
	globalHook := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-hop", "hooks", "pre-repair")
	writeHookScript(t, globalHook, "#!/bin/sh\ntouch \""+marker+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("global pre-repair hook did not fire: %v", err)
	}
}

// Repo level wins over global, per the runner's priority order.
func TestRunRepairHook_RepoLevelBeatsGlobal(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	out := t.TempDir()
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\ntouch \""+filepath.Join(out, "repo")+"\"\n")
	writeHookScript(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\ntouch \""+filepath.Join(out, "global")+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "repo")); err != nil {
		t.Fatalf("repo-level hook should have won: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "global")); err == nil {
		t.Fatal("global hook fired even though a repo-level hook exists")
	}
}

// The four standard vars must reach the hook process. Repair reports the
// hub as GIT_HOP_WORKTREE_PATH and leaves GIT_HOP_BRANCH empty because a
// repair run spans every branch.
func TestRunRepairHook_SuppliesStandardEnv(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	dump := filepath.Join(t.TempDir(), "env.txt")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\n{\n"+
			"  echo \"name=$GIT_HOP_HOOK_NAME\"\n"+
			"  echo \"worktree=$GIT_HOP_WORKTREE_PATH\"\n"+
			"  echo \"repo=$GIT_HOP_REPO_ID\"\n"+
			"  echo \"branch=[$GIT_HOP_BRANCH]\"\n"+
			"} > \""+dump+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}

	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		"name=pre-repair",
		"worktree=" + hubPath,
		"repo=github.com/acme/widgets",
		"branch=[]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected hook env to contain %q, got:\n%s", want, got)
		}
	}
}

// A damaged hop.json is the situation repair exists to fix, so a config
// that parses but carries no org/repo must not break hook dispatch: the
// repo ID goes empty (skipping the hopspace level) and repo-level and
// global hooks still fire.
func TestRunRepairHook_EmptyRepoIDWhenHubConfigLacksOrgRepo(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "", "")
	if got := repairHookRepoID(fs, hubPath); got != "" {
		t.Errorf("expected empty repo ID for org-less hub config, got %q", got)
	}

	dump := filepath.Join(t.TempDir(), "env.txt")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\necho \"repo=[$GIT_HOP_REPO_ID]\" > \""+dump+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook must still dispatch with a damaged hub config: %v", err)
	}
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	if !strings.Contains(string(raw), "repo=[]") {
		t.Errorf("expected empty GIT_HOP_REPO_ID, got: %s", raw)
	}
}

// No hop.json at all: repairHookRepoID must degrade to "" rather than
// panic or fabricate an ID.
func TestRepairHookRepoID_MissingHubConfig(t *testing.T) {
	if got := repairHookRepoID(afero.NewOsFs(), t.TempDir()); got != "" {
		t.Errorf("expected empty repo ID when hop.json is missing, got %q", got)
	}
}

// An unresolvable repo ID must not be confusable with an absent one.
// Repair follows the export-empty convention: the runner always sets all
// four standard vars, so the variable is SET and empty. A hook detects
// "could not be determined" with ${VAR+set}, which stays non-empty.
func TestRunRepairHook_UnresolvableFieldsAreSetButEmpty(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "", "")
	dump := filepath.Join(t.TempDir(), "env.txt")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\n{\n"+
			"  echo \"repo_set=${GIT_HOP_REPO_ID+yes}\"\n"+
			"  echo \"repo_val=[$GIT_HOP_REPO_ID]\"\n"+
			"  echo \"branch_set=${GIT_HOP_BRANCH+yes}\"\n"+
			"  echo \"branch_val=[$GIT_HOP_BRANCH]\"\n"+
			"} > \""+dump+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		"repo_set=yes", "repo_val=[]",
		"branch_set=yes", "branch_val=[]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q (export-empty, not omitted), got:\n%s", want, got)
		}
	}
}

// An empty or 2-part repo ID makes hopspace lookup fall through, because
// the runner needs three "/"-separated segments. Documented, expected
// behavior for pre-repair on a damaged hub — and the global hook must
// still fire so the fall-through is graceful rather than a dead end.
func TestRunRepairHook_EmptyRepoIDFallsThroughHopspaceToGlobal(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "", "")
	out := t.TempDir()

	// A hopspace hook that WOULD match if the repo ID resolved.
	writeHookScript(t, filepath.Join(
		os.Getenv("GIT_HOP_DATA_HOME"), "github.com", "acme", "widgets", "hooks", "pre-repair"),
		"#!/bin/sh\ntouch \""+filepath.Join(out, "hopspace")+"\"\n")
	writeHookScript(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\ntouch \""+filepath.Join(out, "global")+"\"\n")

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "hopspace")); err == nil {
		t.Error("hopspace hook must not resolve without a 3-part repo ID")
	}
	if _, err := os.Stat(filepath.Join(out, "global")); err != nil {
		t.Fatalf("global hook must still fire after hopspace fall-through: %v", err)
	}
}

// post-repair runs after the fix, so it reads the REPAIRED hub config.
// Resolving the repo ID at dispatch time (not once up front) is what
// makes that true: rewriting hop.json between the two calls changes what
// post-repair sees.
func TestFirePostRepairHook_ResolvesRepoIDFromRepairedState(t *testing.T) {
	skipOnWindows(t)

	// Hub starts damaged: org/repo blank, as pre-repair would observe.
	hubPath, fs := repairHookHub(t, "", "")
	out := t.TempDir()
	preDump := filepath.Join(out, "pre.txt")
	postDump := filepath.Join(out, "post.txt")

	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\necho \"repo=[$GIT_HOP_REPO_ID]\" > \""+preDump+"\"\n")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "post-repair"),
		"#!/bin/sh\necho \"repo=[$GIT_HOP_REPO_ID]\" > \""+postDump+"\"\n")

	if err := firePreRepairHook(fs, hubPath); err != nil {
		t.Fatalf("firePreRepairHook: %v", err)
	}

	// Stand in for the repair itself: hop.json now carries org/repo.
	repaired := `{"repo":{"uri":"https://github.com/acme/widgets","org":"acme",` +
		`"repo":"widgets","defaultBranch":"main"},"branches":{},"settings":{}}`
	if err := os.WriteFile(filepath.Join(hubPath, "hop.json"), []byte(repaired), 0o644); err != nil {
		t.Fatalf("rewrite hop.json: %v", err)
	}

	if err := firePostRepairHook(fs, hubPath); err != nil {
		t.Fatalf("firePostRepairHook: %v", err)
	}

	pre, err := os.ReadFile(preDump)
	if err != nil {
		t.Fatalf("read pre dump: %v", err)
	}
	if !strings.Contains(string(pre), "repo=[]") {
		t.Errorf("pre-repair should see the damaged (empty) repo ID, got: %s", pre)
	}

	post, err := os.ReadFile(postDump)
	if err != nil {
		t.Fatalf("read post dump: %v", err)
	}
	if !strings.Contains(string(post), "repo=[github.com/acme/widgets]") {
		t.Errorf("post-repair should see the repaired repo ID, got: %s", post)
	}
}

// With a valid hub config, post-repair gets the full 3-part ID, so
// hopspace-level post-repair hooks actually resolve.
func TestFirePostRepairHook_FiresHopspaceLevelHook(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	marker := filepath.Join(t.TempDir(), "post-hopspace.fired")
	writeHookScript(t, filepath.Join(
		os.Getenv("GIT_HOP_DATA_HOME"), "github.com", "acme", "widgets", "hooks", "post-repair"),
		"#!/bin/sh\ntouch \""+marker+"\"\n")

	if err := firePostRepairHook(fs, hubPath); err != nil {
		t.Fatalf("firePostRepairHook: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hopspace-level post-repair hook did not fire: %v", err)
	}
}

// The hook must NOT be handed the hub as its working directory. Repair
// hooks inherit git-hop's cwd like every other hook; this pins the
// alignment so a future change cannot quietly reintroduce the exception.
func TestRunRepairHook_InheritsCwdRatherThanHub(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	out := t.TempDir()
	dump := filepath.Join(out, "cwd.txt")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\npwd -P > \""+dump+"\"\n")

	// Run git-hop's process from a directory that is NOT the hub.
	elsewhere := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(elsewhere); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("runRepairHook: %v", err)
	}

	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read cwd dump: %v", err)
	}
	got := strings.TrimSpace(string(raw))

	resolvedHub, err := filepath.EvalSymlinks(hubPath)
	if err != nil {
		t.Fatalf("resolve hub: %v", err)
	}
	if got == resolvedHub {
		t.Errorf("hook ran with cwd pinned to the hub (%s); it must inherit git-hop's cwd", got)
	}

	resolvedElsewhere, err := filepath.EvalSymlinks(elsewhere)
	if err != nil {
		t.Fatalf("resolve cwd: %v", err)
	}
	if got != resolvedElsewhere {
		t.Errorf("expected hook cwd %s (inherited), got %s", resolvedElsewhere, got)
	}
}

// A failing pre-repair hook must surface an error so the caller aborts
// before backup or apply.
func TestFirePreRepairHook_FailurePropagates(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair"),
		"#!/bin/sh\nexit 7\n")

	err := firePreRepairHook(fs, hubPath)
	if err == nil {
		t.Fatal("expected failing pre-repair hook to return an error so repair aborts")
	}
	if !strings.Contains(err.Error(), "pre-repair") {
		t.Errorf("expected error to name the hook, got: %v", err)
	}
}

// post-repair stays advisory: repairRun discards its error, and the hook
// itself still reports failure for callers that care.
func TestFirePostRepairHook_AdvisoryFailureDoesNotAbort(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	marker := filepath.Join(t.TempDir(), "post.fired")
	writeHookScript(t, filepath.Join(hubPath, ".git-hop", "hooks", "post-repair"),
		"#!/bin/sh\ntouch \""+marker+"\"\nexit 3\n")

	// The command-level contract: repairRun does `_ = firePostRepairHook(...)`.
	// Mirror that here and assert the run still completes.
	_ = firePostRepairHook(fs, hubPath)

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("post-repair hook did not fire: %v", err)
	}
}

// A non-executable hook file is an error, not a silent skip. The old
// hand-rolled path exec'd it anyway and surfaced a raw OS error.
func TestRunRepairHook_NonExecutableHookErrors(t *testing.T) {
	skipOnWindows(t)

	hubPath, fs := repairHookHub(t, "acme", "widgets")
	hookPath := filepath.Join(hubPath, ".git-hop", "hooks", "pre-repair")
	writeHookScript(t, hookPath, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(hookPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	err := runRepairHook(fs, "pre-repair", hubPath)
	if err == nil {
		t.Fatal("expected an error for a non-executable hook file")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Errorf("expected a not-executable error, got: %v", err)
	}
}

// Absent hooks stay silent at every level.
func TestRunRepairHook_NoHookIsSilentSuccess(t *testing.T) {
	hubPath, fs := repairHookHub(t, "acme", "widgets")
	if err := runRepairHook(fs, "pre-repair", hubPath); err != nil {
		t.Fatalf("missing hook must be a silent success, got: %v", err)
	}
	if err := runRepairHook(fs, "post-repair", hubPath); err != nil {
		t.Fatalf("missing hook must be a silent success, got: %v", err)
	}
}

// The runner validates hook names; repair inherits that instead of
// blindly exec'ing whatever path it was handed.
func TestRunRepairHook_RejectsInvalidHookName(t *testing.T) {
	hubPath, fs := repairHookHub(t, "acme", "widgets")
	if err := runRepairHook(fs, "pre-repare", hubPath); err == nil {
		t.Fatal("expected a typo'd hook name to be rejected by name validation")
	}
}
