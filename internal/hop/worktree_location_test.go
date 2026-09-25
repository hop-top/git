package hop_test

import (
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

func locationCtx(hubPath string) hop.WorktreeLocationContext {
	return hop.WorktreeLocationContext{
		HubPath:  hubPath,
		Branch:   "feat/x",
		Org:      "acme",
		Repo:     "widgets",
		URI:      "git@gitlab.example.com:acme/widgets.git",
		DataHome: "/data",
	}
}

// The centralized default puts worktrees in the repository's hopspace,
// so they follow hop.dataLayout.
func TestExpandWorktreeLocation_CentralizedFollowsDataLayout(t *testing.T) {
	cases := map[string]string{
		"{org}/{repo}":        filepath.Join("/data", "acme", "widgets", "hops", "feat", "x"),
		"{host}/{org}/{repo}": filepath.Join("/data", "gitlab.example.com", "acme", "widgets", "hops", "feat", "x"),
	}
	for layout, want := range cases {
		t.Run(layout, func(t *testing.T) {
			setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: layout})
			if got := hop.ExpandWorktreeLocation("", locationCtx("/hub")); got != want {
				t.Fatalf("ExpandWorktreeLocation(\"\") = %s, want %s", got, want)
			}
		})
	}
}

func TestExpandWorktreeLocation_HopspacePlaceholder(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: hostLayout})
	got := hop.ExpandWorktreeLocation("{hopspace}/trees/{branch}", locationCtx("/hub"))
	if want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets", "trees", "feat", "x"); got != want {
		t.Fatalf("ExpandWorktreeLocation({hopspace}) = %s, want %s", got, want)
	}
}

// {hopspace} uses the hub's own hop.dataLayout, like every other
// data-home path of the repository.
func TestExpandWorktreeLocation_HopspaceUsesHubLayout(t *testing.T) {
	setGlobalGitConfig(t, nil)
	hub := initRepo(t, map[string]string{config.KeyDataLayout: hostLayout})
	got := hop.ExpandWorktreeLocation("", locationCtx(hub))
	if want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets", "hops", "feat", "x"); got != want {
		t.Fatalf("ExpandWorktreeLocation(\"\") in a hub with its own layout = %s, want %s", got, want)
	}
}

// Templates written before {hopspace} expand as they always did.
func TestExpandWorktreeLocation_ExistingTemplatesUnchanged(t *testing.T) {
	setGlobalGitConfig(t, map[string]string{config.KeyDataLayout: hostLayout})
	cases := map[string]string{
		"{hubPath}/hops/{branch}":               filepath.Join("/hub", "hops", "feat", "x"),
		"hops/{branch}":                         filepath.Join("/hub", "hops", "feat", "x"),
		"{dataHome}/{org}/{repo}/hops/{branch}": filepath.Join("/data", "acme", "widgets", "hops", "feat", "x"),
		"/abs/{repo}/{branch}":                  filepath.Join("/abs", "widgets", "feat", "x"),
	}
	for pattern, want := range cases {
		if got := hop.ExpandWorktreeLocation(pattern, locationCtx("/hub")); got != want {
			t.Errorf("ExpandWorktreeLocation(%q) = %s, want %s", pattern, got, want)
		}
	}
}
