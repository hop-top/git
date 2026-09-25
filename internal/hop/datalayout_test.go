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
}

func TestParseHostFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/widgets.git":          "github.com",
		"https://user:tok@GitLab.Example.com/a/b.git":  "gitlab.example.com",
		"ssh://git@gitlab.example.com:2222/acme/w.git": "gitlab.example.com",
		"git@bitbucket.org:acme/widgets.git":           "bitbucket.org",
		"gitea.local:acme/widgets.git":                 "gitea.local",
		"file:///srv/git/acme/widgets.git":             "",
		"/srv/git/acme/widgets.git":                    "",
		"./relative/acme:widgets":                      "",
		"":                                             "",
	}
	for in, want := range cases {
		if got := hop.ParseHostFromURL(in); got != want {
			t.Errorf("ParseHostFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLegacyHooksDir_UsesRepoIDHost(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", "/data")
	got := hop.LegacyHooksDir("github.com/acme/widgets")
	if want := filepath.Join("/data", "github.com", "acme", "widgets", "hooks"); got != want {
		t.Fatalf("LegacyHooksDir() = %s, want %s", got, want)
	}
	if got := hop.LegacyHooksDir("acme/widgets"); got != "" {
		t.Fatalf("LegacyHooksDir(2-part) = %s, want empty", got)
	}
}
