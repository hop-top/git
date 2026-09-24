package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

// These tests pin how doctor --fix repairs a hub branch whose worktree
// directory was removed by hand. git keeps such a worktree registered, so
// the hub check's `git worktree add` failed, while the state check in the
// same run dropped the entry of a merged branch anyway: every run
// reported a failed repair and exited 1.

// registryGit is a MockGit that models the part of git's worktree
// registry these repairs depend on: add refuses a path that is still
// registered, remove drops the registration, and a successful add creates
// the directory. calls records remove/add in order.
type registryGit struct {
	*mocks.MockGit
	fs         afero.Fs
	registered map[string]bool
	calls      []string
}

func newRegistryGit(fs afero.Fs, registered ...string) *registryGit {
	g := &registryGit{MockGit: mocks.NewMockGit(), fs: fs, registered: map[string]bool{}}
	for _, p := range registered {
		g.registered[p] = true
	}
	return g
}

func (g *registryGit) WorktreeRemove(base, path string, force bool) error {
	call := "remove " + path
	if force {
		call += " --force"
	}
	g.calls = append(g.calls, call)
	delete(g.registered, path)
	return g.MockGit.WorktreeRemove(base, path, force)
}

func (g *registryGit) CreateWorktree(base, branch, path, start string, forceCreate bool, track string) error {
	g.calls = append(g.calls, "add "+path)
	if g.registered[path] {
		return errors.New("fatal: '" + path + "' is a missing but already registered worktree")
	}
	g.registered[path] = true
	if err := g.fs.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return g.MockGit.CreateWorktree(base, branch, path, start, forceCreate, track)
}

// porcelainEntry renders one `git worktree list --porcelain` record.
func porcelainEntry(path, branch string, extra ...string) string {
	s := "worktree " + path + "\nHEAD 0123456789abcdef0123456789abcdef01234567\nbranch refs/heads/" + branch + "\n"
	for _, line := range extra {
		s += line + "\n"
	}
	return s + "\n"
}

// mergedInto makes `git branch --merged <base>`, run in the hub, list
// branches.
func mergedInto(g *registryGit, hubPath, base string, branches ...string) {
	out := ""
	for _, b := range branches {
		out += "  " + b + "\n"
	}
	g.Runner.Responses[hubPath+":git branch --merged "+base] = out
}

func worktreeDir(hubPath, branch string) string {
	return filepath.Join(hubPath, config.MakeWorktreePath(branch))
}

// recordMessages returns the messages of r's records of kind about subject.
func recordMessages(r doctorReport, kind, subject string) []string {
	var msgs []string
	for _, rec := range r.records {
		if rec.Kind == kind && rec.Subject == subject {
			msgs = append(msgs, rec.Message)
		}
	}
	return msgs
}

func TestDoctorFix_MissingUnmergedWorktree_ClearsRegistrationThenRecreates(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
	gone := worktreeDir(hubPath, "feat/gone")

	g := newRegistryGit(fs, gone)
	g.WorktreeListOut = porcelainEntry(gone, "feat/gone", "prunable gitdir file points to non-existent location")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.Equal(t, []string{"remove " + gone, "add " + gone}, g.calls,
		"the stale registration must be cleared (without --force) before the worktree is re-added")
	assert.NoError(t, doctorResult(r), "every issue was fixed: %+v", r.records)
	assert.Contains(t, hubBranchKeys(t, fs, hubPath), "feat/gone",
		"a recreated worktree keeps its hop.json row")
}

func TestDoctorFix_MissingMergedWorktree_CleansUpWithoutRecreating(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
	gone := worktreeDir(hubPath, "feat/gone")

	g := newRegistryGit(fs, gone)
	g.WorktreeListOut = porcelainEntry(gone, "feat/gone", "prunable gitdir file points to non-existent location")
	mergedInto(g, hubPath, "main", "feat/gone", "main")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.Equal(t, []string{"remove " + gone}, g.calls,
		"a merged branch's worktree is cleaned up, not recreated")
	exists, _ := afero.DirExists(fs, gone)
	assert.False(t, exists)
	assert.ElementsMatch(t, []string{"main"}, hubBranchKeys(t, fs, hubPath),
		"the merged branch's hop.json row must be dropped")
	assert.NoError(t, doctorResult(r), "every issue was fixed: %+v", r.records)
}

// TestDoctorFix_MissingDefaultBranchWorktree_Recreated: the default branch
// is merged into itself, which must not make its worktree cleanup.
func TestDoctorFix_MissingDefaultBranchWorktree_Recreated(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main"}, nil)
	mainDir := worktreeDir(hubPath, "main")

	g := newRegistryGit(fs)
	mergedInto(g, hubPath, "main", "main")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.Equal(t, []string{"add " + mainDir}, g.calls)
	assert.NoError(t, doctorResult(r), "every issue was fixed: %+v", r.records)
}

// TestDoctorFix_PresentWorktree_Untouched guards the inverse direction: a
// worktree whose directory exists is never unregistered or re-added, even
// if the registry (stale, or racing) claims it is prunable.
func TestDoctorFix_PresentWorktree_Untouched(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/here"}, []string{"main", "feat/here"})
	here := worktreeDir(hubPath, "feat/here")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(here, "notes.txt"), []byte("keep"), 0o644))

	g := newRegistryGit(fs, here)
	g.WorktreeListOut = porcelainEntry(here, "feat/here", "prunable gitdir file points to non-existent location")
	mergedInto(g, hubPath, "main", "feat/here", "main")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.Empty(t, g.calls)
	assert.Empty(t, g.WorktreeRemoveCalls)
	data, err := afero.ReadFile(fs, filepath.Join(here, "notes.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
	assert.Contains(t, hubBranchKeys(t, fs, hubPath), "feat/here")
	assert.NoError(t, doctorResult(r))
}

// TestDoctorFix_MissingWorktree_LeavesRegistrationGitKeeps: only a record
// git marks prunable for this very path is cleared. A locked worktree is
// never prunable, and another path's stale record is not this repair's.
func TestDoctorFix_MissingWorktree_LeavesRegistrationGitKeeps(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
	gone := worktreeDir(hubPath, "feat/gone")

	g := newRegistryGit(fs, gone)
	g.WorktreeListOut = porcelainEntry(gone, "feat/gone", "locked on a removable drive") +
		porcelainEntry("/elsewhere/other", "other", "prunable gitdir file points to non-existent location")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.Empty(t, g.WorktreeRemoveCalls, "no registration may be removed")
	assert.Equal(t, []string{"add " + gone}, g.calls)
	assert.Error(t, doctorResult(r), "git's refusal is reported as a failed repair")
}

// TestDoctorDryRun_MissingWorktree previews both repairs through the same
// logic as a realDir run: the same would-fix records, the same exit status,
// and no git mutation.
func TestDoctorDryRun_MissingWorktree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		merged bool
		want   []string // would-fix messages about the branch
		reject string   // a would-fix message that must not appear
	}{
		{"merged", true, []string{"prune hop.json entry"}, "recreate worktree"},
		{"unmerged", false, []string{"recreate worktree"}, "prune hop.json entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateDoctorPaths(t)
			fs := afero.NewMemMapFs()
			hubPath := "/hubs/repo"
			doctorHub(t, fs, hubPath, []string{"main", "feat/gone"}, []string{"main"})
			gone := worktreeDir(hubPath, "feat/gone")

			g := newRegistryGit(fs, gone)
			g.WorktreeListOut = porcelainEntry(gone, "feat/gone", "prunable gitdir file points to non-existent location")
			if tc.merged {
				mergedInto(g, hubPath, "main", "feat/gone", "main")
			}

			r := runDoctor(fs, g, hubPath, doctorOpts{fix: true, dryRun: true})

			assert.Empty(t, g.calls, "a preview runs no git worktree add/remove")
			assert.Equal(t, []string{"clear stale worktree registration"},
				recordMessages(r, doctorKindWouldFix, gone))
			fixes := strings.Join(recordMessages(r, doctorKindWouldFix, "feat/gone"), "\n")
			for _, want := range tc.want {
				assert.Contains(t, fixes, want)
			}
			assert.NotContains(t, fixes, tc.reject)
			assert.NoError(t, doctorResult(r), "every issue would be fixed: %+v", r.records)

			exists, _ := afero.DirExists(fs, gone)
			assert.False(t, exists, "a preview creates no directory")
			assert.Contains(t, hubBranchKeys(t, fs, hubPath), "feat/gone", "a preview leaves hop.json alone")
		})
	}
}

// TestPrunableWorktree_ResolvesSymlinkedParents: git records worktree
// paths resolved, hop.json may not, and the worktree itself is missing.
func TestPrunableWorktree_ResolvesSymlinkedParents(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	realDir := filepath.Join(root, "realDir")
	require.NoError(t, os.MkdirAll(filepath.Join(realDir, "hops"), 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(realDir, link))

	missing := filepath.Join(link, "hops", "feat", "gone")
	recorded := filepath.Join(realDir, "hops", "feat", "gone")

	assert.True(t, prunableWorktree(porcelainEntry(recorded, "feat/gone", "prunable gitdir file points to non-existent location"), missing))
	assert.True(t, prunableWorktree(porcelainEntry(recorded, "feat/gone", "prunable"), missing))
	assert.False(t, prunableWorktree(porcelainEntry(recorded, "feat/gone"), missing), "a record git does not mark prunable")
	assert.False(t, prunableWorktree(porcelainEntry(recorded, "feat/gone", "locked"), missing), "a locked record")
	assert.False(t, prunableWorktree(porcelainEntry(filepath.Join(realDir, "hops", "other"), "other", "prunable"), missing), "another path's record")
}
