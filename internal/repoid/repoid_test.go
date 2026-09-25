package repoid_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}

// setGitDomain points --global git config at a fresh file, holding
// hop.gitDomain when domain is not empty.
func setGitDomain(t *testing.T, domain string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", file)
	if domain == "" {
		return
	}
	if out, err := exec.Command("git", "config", "--global", config.KeyGitDomain, domain).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
}

func TestHost(t *testing.T) {
	cases := map[string]string{
		"https://gitlab.example.com/acme/widgets.git":       "gitlab.example.com",
		"https://user:pw@GitLab.Example.com/acme/widgets":   "gitlab.example.com",
		"ssh://git@gitlab.example.com:2222/acme/widgets":    "gitlab.example.com",
		"git@gitlab.example.com:acme/widgets.git":           "gitlab.example.com",
		"gitlab.example.com:acme/widgets.git":               "gitlab.example.com",
		"file:///srv/git/acme/widgets.git":                  "",
		"/srv/git/acme/widgets.git":                         "",
		"./acme/widgets":                                    "",
		"../a:b/widgets":                                    "",
		"":                                                  "",
		"git@bitbucket.org:acme/widgets.git":                "bitbucket.org",
		"gitea.local:acme/widgets.git":                      "gitea.local",
		"./relative/acme:widgets":                           "",
		"  https://bitbucket.org/acme/widgets.git  ":        "bitbucket.org",
		"https://github.com/acme/widgets.git":               "github.com",
		"git@github.com:acme/widgets.git":                   "github.com",
		"ssh://git@codeberg.org/acme/widgets.git":           "codeberg.org",
		"http://git.corp.example:8080/scm/acme/widgets.git": "git.corp.example",
	}
	for in, want := range cases {
		if got := repoid.Host(in); got != want {
			t.Errorf("Host(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNew_TakesHostFromOrigin(t *testing.T) {
	setGitDomain(t, "")
	for _, uri := range []string{
		"https://gitlab.example.com/acme/widgets.git",
		"ssh://git@gitlab.example.com:2222/acme/widgets.git",
		"git@gitlab.example.com:acme/widgets.git",
	} {
		if got, want := repoid.New(uri, "acme", "widgets"), "gitlab.example.com/acme/widgets"; got != want {
			t.Errorf("New(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestNew_NoHostFallsBackToGitDomain(t *testing.T) {
	setGitDomain(t, "")
	for _, uri := range []string{"", "/srv/git/acme/widgets.git", "file:///srv/git/acme/widgets.git"} {
		if got, want := repoid.New(uri, "acme", "widgets"), "github.com/acme/widgets"; got != want {
			t.Errorf("default gitDomain: New(%q) = %q, want %q", uri, got, want)
		}
	}

	setGitDomain(t, "git.corp.example")
	for _, uri := range []string{"", "/srv/git/acme/widgets.git", "file:///srv/git/acme/widgets.git"} {
		if got, want := repoid.New(uri, "acme", "widgets"), "git.corp.example/acme/widgets"; got != want {
			t.Errorf("hop.gitDomain set: New(%q) = %q, want %q", uri, got, want)
		}
	}
	// An origin with a host wins over hop.gitDomain.
	if got, want := repoid.New("https://github.com/acme/widgets", "acme", "widgets"), "github.com/acme/widgets"; got != want {
		t.Errorf("New(github origin) = %q, want %q", got, want)
	}
}

func TestNewWithDomain_UsesGivenDomain(t *testing.T) {
	setGitDomain(t, "git.corp.example")
	if got, want := repoid.NewWithDomain("", "acme", "widgets", "other.example"), "other.example/acme/widgets"; got != want {
		t.Errorf("NewWithDomain = %q, want %q", got, want)
	}
}

func TestNew_EmptyOrgOrRepoYieldsEmpty(t *testing.T) {
	setGitDomain(t, "")
	for _, c := range [][2]string{{"", "widgets"}, {"acme", ""}, {"", ""}} {
		if got := repoid.New("https://gitlab.example.com/x/y", c[0], c[1]); got != "" {
			t.Errorf("New(org=%q, repo=%q) = %q, want empty", c[0], c[1], got)
		}
	}
}

func TestFor(t *testing.T) {
	setGitDomain(t, "")
	got := repoid.For("", config.RepoConfig{URI: "git@gitlab.example.com:acme/widgets.git", Org: "acme", Repo: "widgets"})
	if want := "gitlab.example.com/acme/widgets"; got != want {
		t.Fatalf("For() = %q, want %q", got, want)
	}
}

// initRepo creates a git repository whose local config holds hop.gitDomain
// when domain is not empty.
func initRepo(t *testing.T, domain string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if domain != "" {
		if out, err := exec.Command("git", "-C", dir, "config", config.KeyGitDomain, domain).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v\n%s", err, out)
		}
	}
	return dir
}

// setCommandGitDomain sets hop.gitDomain the way `git -c` does, for every
// git the test runs.
func setCommandGitDomain(t *testing.T, domain string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", config.KeyGitDomain)
	t.Setenv("GIT_CONFIG_VALUE_0", domain)
}

const localOrigin = "/srv/git/acme/widgets.git"

// hop.gitDomain resolves like hop.dataLayout: the hub's own value over
// --global.
func TestGitDomainIn_RepoValueOverridesGlobal(t *testing.T) {
	setGitDomain(t, "global.example")
	repo := initRepo(t, "hub.example")

	if got := repoid.GitDomainIn(repo); got != "hub.example" {
		t.Fatalf("GitDomainIn(repo) = %q, want the local hub.example", got)
	}
	cfg := config.RepoConfig{URI: localOrigin, Org: "acme", Repo: "widgets"}
	if got, want := repoid.For(repo, cfg), "hub.example/acme/widgets"; got != want {
		t.Fatalf("For(hub) = %q, want %q", got, want)
	}
	if got, want := repoid.NewIn(repo, localOrigin, "acme", "widgets"), "hub.example/acme/widgets"; got != want {
		t.Fatalf("NewIn(hub) = %q, want %q", got, want)
	}
	// An origin with a host wins over any hop.gitDomain.
	if got, want := repoid.NewIn(repo, "https://gitlab.example.com/acme/widgets", "acme", "widgets"), "gitlab.example.com/acme/widgets"; got != want {
		t.Fatalf("NewIn(hub, gitlab origin) = %q, want %q", got, want)
	}
}

func TestGitDomainIn_GlobalAppliesWhenRepoUnset(t *testing.T) {
	setGitDomain(t, "global.example")
	repo := initRepo(t, "")
	if got := repoid.GitDomainIn(repo); got != "global.example" {
		t.Fatalf("GitDomainIn(repo) = %q, want the --global global.example", got)
	}
}

// Without repository context only --global (and `git -c`) count, even
// when the process runs inside a repository with its own value.
func TestGitDomainIn_NoRepoIgnoresLocalValue(t *testing.T) {
	setGitDomain(t, "global.example")
	repo := initRepo(t, "hub.example")
	t.Chdir(repo)

	if got := repoid.GitDomain(); got != "global.example" {
		t.Fatalf("GitDomain() in a repo = %q, want the --global global.example", got)
	}
	if got, want := repoid.New(localOrigin, "acme", "widgets"), "global.example/acme/widgets"; got != want {
		t.Fatalf("New() in a repo = %q, want %q", got, want)
	}
}

// `git -c` overrides both the hub's and the --global value.
func TestGitDomainIn_CommandOverridesBoth(t *testing.T) {
	setGitDomain(t, "global.example")
	repo := initRepo(t, "hub.example")
	setCommandGitDomain(t, "cmd.example")

	if got := repoid.GitDomainIn(repo); got != "cmd.example" {
		t.Fatalf("GitDomainIn(repo) = %q, want the command-line cmd.example", got)
	}
	if got := repoid.GitDomain(); got != "cmd.example" {
		t.Fatalf("GitDomain() = %q, want the command-line cmd.example", got)
	}
}

// A hub value that is not a host name gives way to --global.
func TestGitDomainIn_InvalidRepoValueFallsBackToGlobal(t *testing.T) {
	setGitDomain(t, "global.example")
	repo := initRepo(t, "not/a/host")
	if got := repoid.GitDomainIn(repo); got != "global.example" {
		t.Fatalf("GitDomainIn(repo) = %q, want the --global global.example", got)
	}
}

func TestValidateGitDomain(t *testing.T) {
	for _, ok := range []string{"github.com", "git.corp.example", "localhost"} {
		if err := repoid.ValidateGitDomain(ok); err != nil {
			t.Errorf("ValidateGitDomain(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "a/b", "git corp"} {
		if err := repoid.ValidateGitDomain(bad); err == nil {
			t.Errorf("ValidateGitDomain(%q) = nil, want an error", bad)
		}
	}
}

func TestSplit(t *testing.T) {
	host, org, repo, ok := repoid.Split("gitlab.example.com/acme/widgets")
	if !ok || host != "gitlab.example.com" || org != "acme" || repo != "widgets" {
		t.Fatalf("Split = %q %q %q %v", host, org, repo, ok)
	}
	for _, bad := range []string{"", "acme/widgets", "a/b/c/d", "/acme/widgets", "h//w", "h/a/"} {
		if _, _, _, ok := repoid.Split(bad); ok {
			t.Errorf("Split(%q) ok, want not ok", bad)
		}
	}
}
