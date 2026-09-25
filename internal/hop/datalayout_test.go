package hop_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// setGlobalGitConfig points --global git config at a fresh file holding
// the given hop.* keys for the rest of the test.
func setGlobalGitConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", file)
	for k, v := range kv {
		if out, err := exec.Command("git", "config", "--global", k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config --global %s %s: %v\n%s", k, v, err, out)
		}
	}
}

var gitlabRef = hop.RepoRef{Host: "gitlab.example.com", Org: "acme", Repo: "widgets"}

func TestGetHopspacePath_DefaultLayoutIsOrgRepo(t *testing.T) {
	setGlobalGitConfig(t, nil)
	got := hop.GetHopspacePath("/data", gitlabRef)
	if want := filepath.Join("/data", "acme", "widgets"); got != want {
		t.Fatalf("GetHopspacePath() = %s, want %s", got, want)
	}
	if hop.DataLayout() != config.Default(config.KeyDataLayout) || hop.DataLayout() != "{org}/{repo}" {
		t.Fatalf("DataLayout() = %q, want the registered default {org}/{repo}", hop.DataLayout())
	}
}

func TestGetHopspacePath_HostLayoutUsesOriginHost(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: "{host}/{org}/{repo}"})
	ref := hop.NewRepoRef("git@gitlab.example.com:acme/widgets.git", "acme", "widgets")
	got := hop.GetHopspacePath("/data", ref)
	if want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets"); got != want {
		t.Fatalf("GetHopspacePath() = %s, want %s", got, want)
	}
}

func TestGetHopspacePath_HostLayoutLocalOriginUsesGitDomain(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{
		config.KeyDataLayout: "{host}/{org}/{repo}",
		config.KeyGitDomain:  "git.corp.example",
	})
	ref := hop.NewRepoRef("/srv/git/acme/widgets.git", "acme", "widgets")
	got := hop.GetHopspacePath("/data", ref)
	if want := filepath.Join("/data", "git.corp.example", "acme", "widgets"); got != want {
		t.Fatalf("GetHopspacePath() = %s, want %s", got, want)
	}
}

func TestDataLayout_InvalidValueFallsBackToDefault(t *testing.T) {
	for _, bad := range []string{"{repo}", "/abs/{org}/{repo}", "{org}/../{repo}", "{owner}/{org}/{repo}"} {
		t.Run(bad, func(t *testing.T) {
			setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: bad})
			if err := hop.ValidateDataLayout(bad); err == nil {
				t.Fatalf("ValidateDataLayout(%q) = nil, want an error", bad)
			}
			if got := hop.DataLayout(); got != "{org}/{repo}" {
				t.Fatalf("DataLayout() = %q, want the default", got)
			}
		})
	}
}

// initRepo creates a git repository whose local config holds kv.
func initRepo(t *testing.T, kv map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for k, v := range kv {
		if out, err := exec.Command("git", "-C", repo, "config", k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config %s %s: %v\n%s", k, v, err, out)
		}
	}
	return repo
}

const hostLayout = "{host}/{org}/{repo}"

func TestResolveDataLayout_RepoValueOverridesGlobal(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: "{org}/{repo}"})
	repo := initRepo(t, map[string]string{config.KeyDataLayout: hostLayout})

	got := hop.ResolveDataLayout(repo)
	if got.Layout != hostLayout || got.Scope != "local" || got.Err != nil {
		t.Fatalf("ResolveDataLayout(repo) = %+v, want the local %s", got, hostLayout)
	}
	path := hop.GetHopspacePath("/data", gitlabRef.In(repo))
	if want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets"); path != want {
		t.Fatalf("GetHopspacePath(ref in repo) = %s, want %s", path, want)
	}
	if other := hop.GetHopspacePath("/data", gitlabRef); other != filepath.Join("/data", "acme", "widgets") {
		t.Fatalf("GetHopspacePath(no repo) = %s, want the --global layout", other)
	}
}

func TestResolveDataLayout_GlobalAppliesWhenRepoUnset(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: hostLayout})
	repo := initRepo(t, nil)

	got := hop.ResolveDataLayout(repo)
	if got.Layout != hostLayout || got.Scope != "global" {
		t.Fatalf("ResolveDataLayout(repo) = %+v, want the --global %s", got, hostLayout)
	}
}

func TestResolveDataLayout_InvalidRepoValueFallsBackToGlobal(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: hostLayout})
	repo := initRepo(t, map[string]string{config.KeyDataLayout: "{repo}"})

	got := hop.ResolveDataLayout(repo)
	if got.Layout != hostLayout || got.Err == nil || got.Scope != "local" || got.Raw != "{repo}" {
		t.Fatalf("ResolveDataLayout(repo) = %+v, want the --global %s with the local value reported invalid", got, hostLayout)
	}
}

func TestResolveDataLayout_InvalidRepoAndGlobalFallBackToDefault(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: "/abs/{org}/{repo}"})
	repo := initRepo(t, map[string]string{config.KeyDataLayout: "{repo}"})

	if got := hop.ResolveDataLayout(repo); got.Layout != "{org}/{repo}" || got.Err == nil {
		t.Fatalf("ResolveDataLayout(repo) = %+v, want the default", got)
	}
}

// `git -c hop.dataLayout=...` reaches git-hop as GIT_CONFIG_PARAMETERS
// (or GIT_CONFIG_COUNT) and overrides the repository and --global.
func TestResolveDataLayout_CommandLineValueWins(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: "{org}/{repo}"})
	repo := initRepo(t, map[string]string{config.KeyDataLayout: "{org}/{repo}"})
	t.Setenv("GIT_CONFIG_PARAMETERS", "'"+config.KeyDataLayout+"'='"+hostLayout+"'")

	for _, dir := range []string{repo, ""} {
		if got := hop.ResolveDataLayout(dir); got.Layout != hostLayout || got.Scope != "command" {
			t.Fatalf("ResolveDataLayout(%q) = %+v, want the command-line %s", dir, got, hostLayout)
		}
	}
}

func TestResolveDataLayout_MissingRepoDirUsesGlobal(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: hostLayout})
	if got := hop.ResolveDataLayout(filepath.Join(t.TempDir(), "gone")); got.Layout != hostLayout {
		t.Fatalf("ResolveDataLayout(missing dir) = %+v, want the --global %s", got, hostLayout)
	}
}

func TestDataLayout_IgnoresRepositoryConfig(t *testing.T) {
	setGlobalGitConfig(t, nil)
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "config", config.KeyDataLayout, "{host}/{org}/{repo}").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	t.Chdir(repo)
	if got := hop.DataLayout(); got != "{org}/{repo}" {
		t.Fatalf("DataLayout() = %q inside a repo with a local value, want the --global default", got)
	}
	if got := hop.ResolveDataLayout("").Layout; got != "{org}/{repo}" {
		t.Fatalf("ResolveDataLayout(\"\") = %q inside a repo with a local value, want the --global default", got)
	}
}

// Releases before repo IDs carried the origin's host kept every
// repository's hooks under <data>/github.com, whatever its origin.
func TestLegacyHooksDir_AlwaysUnderGitHub(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", "/data")
	want := filepath.Join("/data", "github.com", "acme", "widgets", "hooks")
	for _, id := range []string{"github.com/acme/widgets", "gitlab.example.com/acme/widgets"} {
		if got := hop.LegacyHooksDir(id); got != want {
			t.Fatalf("LegacyHooksDir(%s) = %s, want %s", id, got, want)
		}
	}
	if got := hop.LegacyHooksDir("acme/widgets"); got != "" {
		t.Fatalf("LegacyHooksDir(2-part) = %s, want empty", got)
	}
}
