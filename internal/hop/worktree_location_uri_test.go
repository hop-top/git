package hop

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

// The origin URL the hopspace records reaches {hopspace}: under
// {host}/{org}/{repo}, worktrees of a gitlab.example.com repository land
// under that host, not under hop.gitDomain.
func TestCreateWorktree_CentralizedUsesHopspaceOriginHost(t *testing.T) {
	gc := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gc)
	if out, err := exec.Command("git", "config", "--global", config.KeyDataLayout, "{host}/{org}/{repo}").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	t.Setenv("GIT_HOP_DATA_HOME", "/data")

	for name, create := range map[string]func(m *WorktreeManager, hs *Hopspace) (string, error){
		"CreateWorktree": func(m *WorktreeManager, hs *Hopspace) (string, error) {
			return m.CreateWorktree(hs, "/hub", "feat/x", "", "acme", "widgets", "main", "")
		},
		"CreateWorktreeTransactional": func(m *WorktreeManager, hs *Hopspace) (string, error) {
			return m.CreateWorktreeTransactional(hs, "/hub", "feat/x", "", "acme", "widgets", "main", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			if err := fs.MkdirAll("/hub/hops/main", 0o755); err != nil {
				t.Fatal(err)
			}
			hs := &Hopspace{
				Path: "/hub",
				Config: &config.HopspaceConfig{
					Repo: config.RepoConfig{URI: "git@gitlab.example.com:acme/widgets.git", Org: "acme", Repo: "widgets"},
					Branches: map[string]config.HopspaceBranch{
						"main": {Path: "/hub/hops/main", Exists: true},
					},
				},
				fs: fs,
			}
			got, err := create(NewWorktreeManager(fs, mocks.NewMockGit()), hs)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join("/data", "gitlab.example.com", "acme", "widgets", "hops", "feat", "x"); got != want {
				t.Fatalf("worktree path = %s, want %s", got, want)
			}
		})
	}
}
