package hop

import (
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

// joinGit resolves the refs listed in refs (as passed to rev-parse
// --verify [--quiet]) and records the CreateWorktree call it receives.
type joinGit struct {
	*mocks.MockGit
	refs map[string]bool

	base        string
	forceCreate bool
	trackBranch string
}

func (j *joinGit) RevParse(dir string, args ...string) (string, error) {
	ref := args[len(args)-1]
	if j.refs[ref] {
		return "0123456789abcdef", nil
	}
	return "", errRefMissing
}

func (j *joinGit) CreateWorktree(hopspacePath, branch, path, base string, forceCreate bool, trackBranch string) error {
	j.base, j.forceCreate, j.trackBranch = base, forceCreate, trackBranch
	return j.MockGit.CreateWorktree(hopspacePath, branch, path, base, forceCreate, trackBranch)
}

type refMissing struct{}

func (refMissing) Error() string { return "fatal: Needed a single revision" }

var errRefMissing error = refMissing{}

func createForJoin(t *testing.T, g *joinGit, enforce bool, startPoint string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("/hub/hops/main", 0o755); err != nil {
		t.Fatal(err)
	}
	hs := &Hopspace{
		Path: "/hub",
		Config: &config.HopspaceConfig{Branches: map[string]config.HopspaceBranch{
			"main": {Path: "/hub/hops/main", Exists: true},
		}},
		fs: fs,
	}
	m := NewWorktreeManager(fs, g)
	m.EnforceStartPoint = enforce
	if _, err := m.CreateWorktree(hs, "/hub", "feat/shared", "{hubPath}/hops/{branch}", "o", "r", "main", startPoint); err != nil {
		t.Fatal(err)
	}
}

// A branch present only as origin/<branch> is created from it, never
// left to git's same-name guess (which gives up when another remote has
// the name too, and then falls back to forking the default branch).
func TestCreateWorktree_JoinsRemoteOnlyBranch(t *testing.T) {
	g := &joinGit{MockGit: mocks.NewMockGit(), refs: map[string]bool{
		"refs/remotes/origin/main":        true,
		"refs/remotes/origin/feat/shared": true,
	}}
	createForJoin(t, g, false, "")

	if !g.forceCreate || g.base != "refs/remotes/origin/feat/shared" {
		t.Errorf("CreateWorktree(base=%q, forceCreate=%v), want base=refs/remotes/origin/feat/shared forceCreate=true",
			g.base, g.forceCreate)
	}
}

// A configured default start-point only seeds brand new branches; it
// does not override joining the branch that already exists on origin.
func TestCreateWorktree_JoinWinsOverConfiguredStartPoint(t *testing.T) {
	g := &joinGit{MockGit: mocks.NewMockGit(), refs: map[string]bool{
		"refs/remotes/origin/main":        true,
		"refs/remotes/origin/feat/shared": true,
		"develop":                         true,
	}}
	createForJoin(t, g, false, "develop")

	if !g.forceCreate || g.base != "refs/remotes/origin/feat/shared" {
		t.Errorf("CreateWorktree(base=%q, forceCreate=%v), want the origin branch", g.base, g.forceCreate)
	}
}

// Inverse guard: an existing local branch is linked as-is.
func TestCreateWorktree_LocalBranchIsLinkedNotJoined(t *testing.T) {
	g := &joinGit{MockGit: mocks.NewMockGit(), refs: map[string]bool{
		"refs/remotes/origin/main":        true,
		"refs/heads/feat/shared":          true,
		"refs/remotes/origin/feat/shared": true,
	}}
	createForJoin(t, g, false, "")

	if g.forceCreate {
		t.Errorf("forceCreate = true for an existing local branch; want it linked (base=%q)", g.base)
	}
}

// Inverse guard: an explicit --from keeps its contract (create from it,
// never from a same-named remote branch).
func TestCreateWorktree_ExplicitStartPointBeatsJoin(t *testing.T) {
	g := &joinGit{MockGit: mocks.NewMockGit(), refs: map[string]bool{
		"refs/remotes/origin/main":        true,
		"refs/remotes/origin/feat/shared": true,
		"main^{commit}":                   true,
	}}
	createForJoin(t, g, true, "main")

	if !g.forceCreate || g.base != "main" {
		t.Errorf("CreateWorktree(base=%q, forceCreate=%v), want base=main forceCreate=true", g.base, g.forceCreate)
	}
}

// Inverse guard: a brand new branch still starts from the default branch.
func TestCreateWorktree_NewBranchUnaffected(t *testing.T) {
	g := &joinGit{MockGit: mocks.NewMockGit(), refs: map[string]bool{
		"refs/remotes/origin/main": true,
	}}
	createForJoin(t, g, false, "")

	if g.forceCreate || g.base != "refs/remotes/origin/main" {
		t.Errorf("CreateWorktree(base=%q, forceCreate=%v), want base=refs/remotes/origin/main forceCreate=false",
			g.base, g.forceCreate)
	}
}
