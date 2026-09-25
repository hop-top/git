package hooks

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// withGlobalGitConfig points --global git config at a fresh file holding
// the given keys for the rest of the test.
func withGlobalGitConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", file)
	for k, v := range kv {
		if out, err := exec.Command("git", "config", "--global", k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config --global %s %s: %v\n%s", k, v, err, out)
		}
	}
}

func writeFile(t *testing.T, fs afero.Fs, path, body string) {
	t.Helper()
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

const (
	layoutRepoID = "github.com/acme/widgets"
	gitlabURI    = "https://gitlab.example.com/acme/widgets.git"
)

// The default layout puts a repository's hooks beside its hopspace config,
// at <data>/<org>/<repo>/hooks, not under a github.com directory.
func TestMirror_DefaultLayoutMirrorsUnderOrgRepo(t *testing.T) {
	withGlobalGitConfig(t, nil)
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	writeHook(t, fs, "/wt", "post-worktree-add", "#!/bin/sh\n", 0o755)

	if _, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: "/wt", RepoID: layoutRepoID, RepoURI: gitlabURI, Mode: ModeCopy,
	}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/data", "acme", "widgets", "hooks", "post-worktree-add")
	if ok, _ := afero.Exists(fs, want); !ok {
		t.Fatalf("hook not mirrored to %s", want)
	}
	if ok, _ := afero.DirExists(fs, filepath.Join("/data", "github.com")); ok {
		t.Fatalf("mirror created <data>/github.com for a %s origin", gitlabURI)
	}
}

func TestFindHookFile_DefaultLayoutFindsOrgRepoHooks(t *testing.T) {
	withGlobalGitConfig(t, nil)
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	hook := filepath.Join("/data", "acme", "widgets", "hooks", "pre-worktree-add")
	writeFile(t, fs, hook, "#!/bin/sh\n")

	if got := NewRunner(fs).ForRepo(gitlabURI, "").FindHookFile("pre-worktree-add", "/nowhere", layoutRepoID); got != hook {
		t.Fatalf("FindHookFile() = %q, want %q", got, hook)
	}
}

// Hooks mirrored by earlier releases, at <data>/github.com/<org>/<repo>/hooks,
// keep firing until they are moved.
func TestFindHookFile_LegacyLocationStillResolves(t *testing.T) {
	withGlobalGitConfig(t, nil)
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	legacy := filepath.Join("/data", "github.com", "acme", "widgets", "hooks", "pre-worktree-add")
	writeFile(t, fs, legacy, "#!/bin/sh\n")

	if got := NewRunner(fs).ForRepo(gitlabURI, "").FindHookFile("pre-worktree-add", "/nowhere", layoutRepoID); got != legacy {
		t.Fatalf("FindHookFile() = %q, want the legacy hook %q", got, legacy)
	}
}

func TestFindHookFile_NewLocationWinsOverLegacy(t *testing.T) {
	withGlobalGitConfig(t, nil)
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	legacy := filepath.Join("/data", "github.com", "acme", "widgets", "hooks", "pre-worktree-add")
	current := filepath.Join("/data", "acme", "widgets", "hooks", "pre-worktree-add")
	writeFile(t, fs, legacy, "#!/bin/sh\necho old\n")
	writeFile(t, fs, current, "#!/bin/sh\necho new\n")

	if got := NewRunner(fs).FindHookFile("pre-worktree-add", "/nowhere", layoutRepoID); got != current {
		t.Fatalf("FindHookFile() = %q, want the new-location hook %q", got, current)
	}
}

// With hop.dataLayout={host}/{org}/{repo} the host comes from the origin
// URL, not from the repo ID's fixed github.com.
func TestHostLayout_NonGitHubOriginEndToEnd(t *testing.T) {
	withGlobalGitConfig(t, map[string]string{"hop.dataLayout": "{host}/{org}/{repo}"})
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	writeHook(t, fs, "/wt", "post-worktree-add", "#!/bin/sh\n", 0o755)

	if _, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: "/wt", RepoID: layoutRepoID, RepoURI: gitlabURI, Mode: ModeCopy,
	}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets", "hooks", "post-worktree-add")
	if ok, _ := afero.Exists(fs, want); !ok {
		t.Fatalf("hook not mirrored to %s", want)
	}
	if got := NewRunner(fs).ForRepo(gitlabURI, "").FindHookFile("post-worktree-add", "/nowhere", layoutRepoID); got != want {
		t.Fatalf("FindHookFile() = %q, want %q", got, want)
	}
}

// gitRepoWithLayout creates a real repository whose own config sets
// hop.dataLayout to layout.
func gitRepoWithLayout(t *testing.T, layout string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "hop.dataLayout", layout).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	return dir
}

// A repository's own hop.dataLayout decides where its hooks are mirrored
// and looked up, over --global.
func TestRepoLayout_OverridesGlobalForMirrorAndLookup(t *testing.T) {
	withGlobalGitConfig(t, map[string]string{"hop.dataLayout": "{org}/{repo}"})
	repo := gitRepoWithLayout(t, "{host}/{org}/{repo}")
	fs := afero.NewMemMapFs()
	withDataHome(t, "/data")
	writeHook(t, fs, repo, "post-worktree-add", "#!/bin/sh\n", 0o755)

	if _, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: repo, RepoID: layoutRepoID, RepoURI: gitlabURI, Mode: ModeCopy,
	}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets", "hooks", "post-worktree-add")
	if ok, _ := afero.Exists(fs, want); !ok {
		t.Fatalf("hook not mirrored to %s", want)
	}
	if got := NewRunner(fs).ForRepo(gitlabURI, repo).FindHookFile("post-worktree-add", "/nowhere", layoutRepoID); got != want {
		t.Fatalf("FindHookFile() = %q, want %q", got, want)
	}
}
