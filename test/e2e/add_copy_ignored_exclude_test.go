package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// setupToolStateHub is a hub whose committed .gitignore ignores tracker
// state, a marked dir and ordinary local files, with those files present in
// hops/main.
func setupToolStateHub(t *testing.T) (*TestEnv, string) {
	t.Helper()
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	WriteFile(t, filepath.Join(env.SeedRepoPath, ".gitignore"),
		".env\n.tlc/\n.hop/\nscratch/\nlogs/\n#-hop-#\nmarked/\n")
	env.RunCommand(t, env.SeedRepoPath, "git", "add", ".gitignore")
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "-m", "ignore")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	main := filepath.Join(env.HubPath, "hops", "main")
	for rel, body := range map[string]string{
		".env":                 "S=1\n",
		".tlc/db.sqlite":       "x",
		".hop/tlc/config.yaml": "c",
		"scratch/a":            "a",
		"logs/b":               "b",
		"marked/m":             "m",
	} {
		p := filepath.Join(main, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		WriteFile(t, p, body)
	}
	return env, main
}

var previewLine = regexp.MustCompile(`\[dry-run\]\s+copy (\S+)`)

func previewCopies(out string) []string {
	var got []string
	for _, m := range previewLine.FindAllStringSubmatch(out, -1) {
		got = append(got, m[1])
	}
	sort.Strings(got)
	return got
}

func topLevel(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestAddCopyIgnored_DefaultExcludesToolState(t *testing.T) {
	t.Parallel()
	env, _ := setupToolStateHub(t)
	env.RunCommand(t, env.HubPath, "git", "config", "--add", "hop.add.copyIgnoredExclude", "scratch/")

	dry := env.RunGitHopCombined(t, env.HubPath, "add", "feat/x", "--dry-run")
	for _, want := range []string{
		"skip .tlc (built-in default exclusion",
		"skip .hop (built-in default exclusion",
		"skip scratch (hop.add.copyIgnoredExclude)",
		"skip marked (ignore rule marked #-hop-#)",
	} {
		if !strings.Contains(dry, want) {
			t.Errorf("dry-run missing %q:\n%s", want, dry)
		}
	}
	planned := previewCopies(dry)

	env.RunGitHopCombined(t, env.HubPath, "add", "feat/x")
	wt := filepath.Join(env.HubPath, "hops", "feat", "x")
	for _, rel := range []string{".tlc", ".hop", "scratch", "marked"} {
		if _, err := os.Stat(filepath.Join(wt, rel)); err == nil {
			t.Errorf("%s copied into new worktree", rel)
		}
	}
	var copied []string
	for _, n := range planned {
		if _, err := os.Stat(filepath.Join(wt, n)); err != nil {
			t.Errorf("dry-run promised %s but it is absent: %v", n, err)
		}
		copied = append(copied, n)
	}
	for _, n := range topLevel(t, wt) {
		if n == ".env" || n == "logs" {
			if i := sort.SearchStrings(copied, n); i >= len(copied) || copied[i] != n {
				t.Errorf("%s copied but not in the dry-run listing %v", n, planned)
			}
		}
	}
	if len(planned) != 2 {
		t.Errorf("planned = %v, want [.env logs]", planned)
	}
}

func TestAddCopyIgnored_ConfigOptsBackIn(t *testing.T) {
	t.Parallel()
	env, _ := setupToolStateHub(t)
	env.RunCommand(t, env.HubPath, "git", "config", "--add", "hop.add.copyIgnoredExclude", "!.tlc")

	env.RunGitHopCombined(t, env.HubPath, "add", "feat/x")
	wt := filepath.Join(env.HubPath, "hops", "feat", "x")
	if _, err := os.Stat(filepath.Join(wt, ".tlc", "db.sqlite")); err != nil {
		t.Errorf(".tlc not copied after opt-in: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".hop")); err == nil {
		t.Error(".hop copied; only .tlc was opted back in")
	}
}
