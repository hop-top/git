package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Remove judges each worktree against the branch it was made from, so
// these tests stack worktrees on an integration branch (next) and on a
// parent feature branch, merge them there and not into main, and check
// what remove, remove --merged, --dry-run and doctor make of that.

type baseRec struct {
	removeRec
	CheckedAgainst string `json:"checked_against"`
	CheckedSource  string `json:"checked_against_source"`
	CheckedNote    string `json:"checked_against_note"`
}

func baseRemoveJSON(t *testing.T, env *TestEnv, args ...string) ([]baseRec, string, int) {
	t.Helper()
	full := append([]string{"remove", "--format", "json"}, args...)
	stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, full...)
	var recs []baseRec
	if strings.TrimSpace(stdout) != "" {
		if err := json.Unmarshal([]byte(stdout), &recs); err != nil {
			t.Fatalf("decode %q: %v\nstderr: %s", stdout, err, stderr)
		}
	}
	return recs, stdout + stderr, code
}

func baseRecFor(t *testing.T, recs []baseRec, branch string) baseRec {
	t.Helper()
	for _, r := range recs {
		if r.Kind == "worktree" && r.Branch == branch {
			return r
		}
	}
	t.Fatalf("no worktree record for %q in %+v", branch, recs)
	return baseRec{}
}

func hopWT(env *TestEnv, branch string) string {
	return filepath.Join(env.HubPath, "hops", filepath.FromSlash(branch))
}

// commitFile commits a new file in the worktree of branch.
func commitFile(t *testing.T, env *TestEnv, branch, name string) {
	t.Helper()
	wt := hopWT(env, branch)
	if err := os.WriteFile(filepath.Join(wt, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.RunCommand(t, wt, "git", "add", name)
	env.RunCommand(t, wt, "git", "commit", "-m", "add "+name)
}

// addFrom adds a hop for branch started from base and commits a file in it.
func addFrom(t *testing.T, env *TestEnv, branch, base string) {
	t.Helper()
	env.RunGitHop(t, env.HubPath, "add", branch, "--from", base)
	commitFile(t, env, branch, strings.ReplaceAll(branch, "/", "_")+".txt")
}

// mergeInto merges src into dst in dst's worktree: a merge commit, or a
// squash commit that leaves src's tip unreachable.
func mergeInto(t *testing.T, env *TestEnv, dst, src string, squash bool) {
	t.Helper()
	wt := hopWT(env, dst)
	if squash {
		env.RunCommand(t, wt, "git", "merge", "--squash", src)
		env.RunCommand(t, wt, "git", "commit", "-m", "squash "+src)
		return
	}
	env.RunCommand(t, wt, "git", "merge", "--no-ff", src, "-m", "merge "+src)
}

// baseHub is a hub with main and a hop for the integration branch next.
func baseHub(t *testing.T) *TestEnv {
	t.Helper()
	env := removeHub(t)
	env.RunGitHop(t, env.HubPath, "add", "next", "--from", "main")
	commitFile(t, env, "next", "next.txt")
	return env
}

// editHopJSON rewrites the hub's hop.json.
func editHopJSON(t *testing.T, env *TestEnv, edit func(cfg map[string]any)) {
	t.Helper()
	p := filepath.Join(env.HubPath, "hop.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	edit(cfg)
	out, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func branchEntry(cfg map[string]any, branch string) map[string]any {
	return cfg["branches"].(map[string]any)[branch].(map[string]any)
}

func TestRemoveBase_MergedIntoItsBaseNext_Removed(t *testing.T) {
	t.Parallel()
	for name, squash := range map[string]bool{"merge commit": false, "squash": true} {
		squash := squash
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := baseHub(t)
			addFrom(t, env, "feat/a", "next")
			mergeInto(t, env, "next", "feat/a", squash)

			recs, out, code := baseRemoveJSON(t, env, "--no-prompt", "feat/a")
			if code != 0 {
				t.Fatalf("exit %d, want 0 (merged into its base next, not into main):\n%s", code, out)
			}
			r := baseRecFor(t, recs, "feat/a")
			if !r.Removed || r.CheckedAgainst != "next" || r.CheckedSource != "base" {
				t.Errorf("record = %+v, want removed, checked against next (base)", r)
			}
			if hasBranch(t, env, "feat/a") {
				t.Errorf("branch feat/a left behind")
			}
		})
	}
}

func TestRemoveBase_BaseMainMergedOnlyIntoNext_Refused(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/b", "main")
	mergeInto(t, env, "next", "feat/b", false)

	_, out, code := baseRemoveJSON(t, env, "--no-prompt", "feat/b")
	if code == 0 {
		t.Fatalf("removed a branch not merged into its base main:\n%s", out)
	}
	if !strings.Contains(out, "not merged into 'main' (default)") {
		t.Errorf("refusal does not name main (default):\n%s", out)
	}
	if !hasBranch(t, env, "feat/b") {
		t.Errorf("branch feat/b gone despite the refusal")
	}

	// --base beats the recorded base for the one candidate.
	recs, out, code := baseRemoveJSON(t, env, "--no-prompt", "--base", "next", "feat/b")
	if code != 0 {
		t.Fatalf("--base next: exit %d:\n%s", code, out)
	}
	if r := baseRecFor(t, recs, "feat/b"); r.CheckedAgainst != "next" || r.CheckedSource != "override" {
		t.Errorf("record = %+v, want next (override)", r)
	}
}

func TestRemoveBase_OverrideIgnoredWhenWrongBranch(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/a", "next")
	mergeInto(t, env, "next", "feat/a", false)

	// Merged into its base next, but --base main says otherwise: the
	// override wins over the base, so it is refused.
	_, out, code := baseRemoveJSON(t, env, "--no-prompt", "--base", "main", "feat/a")
	if code == 0 || !strings.Contains(out, "not merged into 'main' (override)") {
		t.Fatalf("exit %d, want refusal naming main (override):\n%s", code, out)
	}

	_, out, code = baseRemoveJSON(t, env, "--no-prompt", "--base", "nope", "feat/a")
	if code == 0 || !strings.Contains(out, "no local branch 'nope'") {
		t.Fatalf("exit %d, want a --base error:\n%s", code, out)
	}
}

func TestRemoveBase_StackChildMergedIntoParent_Removed(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/parent", "main")
	addFrom(t, env, "feat/child", "feat/parent")
	mergeInto(t, env, "feat/parent", "feat/child", true)

	recs, out, code := baseRemoveJSON(t, env, "--no-prompt", "feat/child")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if r := baseRecFor(t, recs, "feat/child"); r.CheckedAgainst != "feat/parent" || r.CheckedSource != "base" {
		t.Errorf("record = %+v, want feat/parent (base)", r)
	}
}

func TestRemoveBase_NoBaseFallsToCompareBranchThenDefault(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/n", "main")
	addFrom(t, env, "feat/m", "main")
	mergeInto(t, env, "next", "feat/n", false)
	mergeInto(t, env, "main", "feat/m", false)
	editHopJSON(t, env, func(cfg map[string]any) {
		delete(branchEntry(cfg, "feat/n"), "base")
		delete(branchEntry(cfg, "feat/m"), "base")
	})

	// Neither records a base: default main. feat/n is merged only into next.
	_, out, code := baseRemoveJSON(t, env, "--no-prompt", "feat/n")
	if code == 0 || !strings.Contains(out, "not merged into 'main' (default)") {
		t.Fatalf("exit %d, want refusal naming main (default):\n%s", code, out)
	}
	recs, out, code := baseRemoveJSON(t, env, "--no-prompt", "feat/m")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if r := baseRecFor(t, recs, "feat/m"); r.CheckedSource != "default" || r.CheckedAgainst != "main" {
		t.Errorf("record = %+v, want main (default)", r)
	}

	// With compareBranch next the same feat/n is merged.
	editHopJSON(t, env, func(cfg map[string]any) {
		s, _ := cfg["settings"].(map[string]any)
		if s == nil {
			s = map[string]any{}
			cfg["settings"] = s
		}
		s["compareBranch"] = "next"
	})
	recs, out, code = baseRemoveJSON(t, env, "--no-prompt", "feat/n")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if r := baseRecFor(t, recs, "feat/n"); r.CheckedSource != "compareBranch" || r.CheckedAgainst != "next" {
		t.Errorf("record = %+v, want next (compareBranch)", r)
	}
}

func TestRemoveBase_DeletedBaseFallsBackAndSaysSo(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	env.RunCommand(t, env.HubPath, "git", "branch", "gone-base", "main")
	addFrom(t, env, "feat/c", "gone-base")
	mergeInto(t, env, "main", "feat/c", false)
	env.RunCommand(t, env.HubPath, "git", "branch", "-D", "gone-base")

	recs, out, code := baseRemoveJSON(t, env, "--no-prompt", "--dry-run", "feat/c")
	if code != 0 {
		t.Fatalf("dry-run exit %d:\n%s", code, out)
	}
	r := baseRecFor(t, recs, "feat/c")
	if r.CheckedAgainst != "main" || r.CheckedSource != "default" ||
		!strings.Contains(r.CheckedNote, "recorded base 'gone-base' no longer exists") {
		t.Errorf("record = %+v, want main (default) with the missing-base note", r)
	}
	if !strings.Contains(out, "recorded base 'gone-base' no longer exists") {
		t.Errorf("output does not say the base is gone:\n%s", out)
	}
}

func TestRemoveBase_DeletedBaseRefusalSaysWhy(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	env.RunCommand(t, env.HubPath, "git", "branch", "gone-base", "main")
	addFrom(t, env, "feat/d", "gone-base")
	env.RunCommand(t, env.HubPath, "git", "branch", "-D", "gone-base")

	_, out, code := baseRemoveJSON(t, env, "--no-prompt", "feat/d")
	if code == 0 {
		t.Fatalf("removed unmerged feat/d:\n%s", out)
	}
	if !strings.Contains(out, "not merged into 'main' (default)") ||
		!strings.Contains(out, "recorded base 'gone-base' no longer exists") {
		t.Errorf("refusal names neither the fallback nor the missing base:\n%s", out)
	}
}

func TestRemoveBase_MergedMixesBasesAndDryRunMatchesRun(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/a", "next")
	addFrom(t, env, "feat/b", "main")
	addFrom(t, env, "feat/z", "main")
	mergeInto(t, env, "next", "feat/a", false)
	mergeInto(t, env, "main", "feat/b", true)
	mergeInto(t, env, "next", "feat/z", false) // next only; its base is main

	summarize := func(recs []baseRec) []string {
		var out []string
		for _, r := range recs {
			if r.Kind == "worktree" {
				out = append(out, r.Branch+" removed="+boolStr(r.Removed)+" vs "+r.CheckedAgainst+" ("+r.CheckedSource+")")
			}
		}
		sort.Strings(out)
		return out
	}

	preview, out, code := baseRemoveJSON(t, env, "--merged", "--no-prompt", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit %d:\n%s", code, out)
	}
	if !hasBranch(t, env, "feat/a") || !hasBranch(t, env, "feat/b") {
		t.Fatalf("dry-run removed something")
	}
	run, out, code := baseRemoveJSON(t, env, "--merged", "--no-prompt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}

	want := []string{
		"feat/a removed=true vs next (base)",
		"feat/b removed=true vs main (default)",
	}
	for name, got := range map[string][]string{"dry-run": summarize(preview), "run": summarize(run)} {
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if hasBranch(t, env, "feat/a") || hasBranch(t, env, "feat/b") {
		t.Errorf("merged branches left behind")
	}
	if !hasBranch(t, env, "feat/z") {
		t.Errorf("feat/z, not merged into its base main, was removed")
	}

	// --base next makes feat/z a candidate for every worktree alike.
	recs, out, code := baseRemoveJSON(t, env, "--merged", "--no-prompt", "--base", "next")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if r := baseRecFor(t, recs, "feat/z"); !r.Removed || r.CheckedSource != "override" {
		t.Errorf("record = %+v, want removed against next (override)", r)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestRemoveBase_DryRunSingleMatchesRun(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/a", "next")
	addFrom(t, env, "feat/b", "main")
	mergeInto(t, env, "next", "feat/a", false)
	mergeInto(t, env, "next", "feat/b", false)

	for _, br := range []string{"feat/a", "feat/b"} {
		_, dry, dcode := baseRemoveJSON(t, env, "--no-prompt", "--dry-run", br)
		_, run, rcode := baseRemoveJSON(t, env, "--no-prompt", br)
		if (dcode == 0) != (rcode == 0) {
			t.Errorf("%s: dry-run exit %d, run exit %d", br, dcode, rcode)
		}
		if dcode != 0 && !strings.Contains(dry, "not merged into 'main' (default)") {
			t.Errorf("%s: dry-run refusal does not name main (base):\n%s", br, dry)
		}
		if rcode != 0 && !strings.Contains(run, "not merged into 'main' (default)") {
			t.Errorf("%s: refusal does not name main (base):\n%s", br, run)
		}
	}
}

func TestRemoveBase_DoctorFollowsTheBase(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/a", "next")
	addFrom(t, env, "feat/b", "main")
	mergeInto(t, env, "next", "feat/a", false)
	mergeInto(t, env, "next", "feat/b", false)
	for _, b := range []string{"feat/a", "feat/b"} {
		if err := os.RemoveAll(hopWT(env, b)); err != nil {
			t.Fatal(err)
		}
	}

	out := env.RunGitHopCombined(t, env.HubPath, "doctor", "--fix")
	if !strings.Contains(out, "feat/a is merged into next (base); not recreating its worktree") {
		t.Errorf("doctor did not treat feat/a as merged into its base next:\n%s", out)
	}
	if strings.Contains(out, "feat/b is merged into") {
		t.Errorf("doctor treated feat/b as merged although its base main lacks it:\n%s", out)
	}
	if _, err := os.Stat(hopWT(env, "feat/a")); err == nil {
		t.Errorf("feat/a worktree recreated")
	}
	if _, err := os.Stat(hopWT(env, "feat/b")); err != nil {
		t.Errorf("feat/b worktree not recreated: %v", err)
	}
}

func TestRemoveBase_StatusStillComparesAgainstBase(t *testing.T) {
	t.Parallel()
	env := baseHub(t)
	addFrom(t, env, "feat/a", "next")

	out := env.RunGitHop(t, env.HubPath, "status", "--format", "json")
	if !strings.Contains(out, `"base": "next"`) && !strings.Contains(out, `"base":"next"`) {
		t.Errorf("status no longer reports base next:\n%s", out)
	}
}
