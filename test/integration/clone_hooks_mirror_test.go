package integration_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// TestMirrorCommittedHooks_Symlink_RealFs exercises the symlink install
// path against a real on-disk worktree containing a committed
// .git-hop/hooks/post-worktree-add. This is the integration counterpart to
// the unit table tests in internal/hooks/install_test.go and the regression
// fixture for T-0217: a fresh hopspace must end up with a working pointer
// to the committed hook so post-worktree-add fires for the first worktree.
func TestMirrorCommittedHooks_Symlink_RealFs(t *testing.T) {
	tempDir := t.TempDir()
	dataHome := filepath.Join(tempDir, "data")
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)

	// Initialize a git repo to mirror the layout cmd/init.go and
	// CloneWorktree produce after a clone.
	mustRun(t, tempDir, "git", "init", "-b", "main")
	mustRun(t, tempDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, tempDir, "git", "config", "user.name", "Test User")

	hooksDir := filepath.Join(tempDir, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "post-worktree-add")
	hookBody := "#!/bin/sh\necho post-worktree-add fired\n"
	if err := os.WriteFile(hookPath, []byte(hookBody), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	// Commit it so the test mirrors the "committed hook" precondition.
	mustRun(t, tempDir, "git", "add", ".git-hop")
	mustRun(t, tempDir, "git", "commit", "-m", "add hook")

	res, err := hooks.MirrorCommittedHooks(afero.NewOsFs(), hooks.MirrorOpts{
		WorktreePath: tempDir,
		RepoID:       "github.com/testorg/testrepo",
		Mode:         hooks.ModeSymlink,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("MirrorCommittedHooks: %v", err)
	}
	if res.Installed != 1 {
		t.Fatalf("expected Installed=1, got %+v", res)
	}

	dst := filepath.Join(dataHome, "github.com", "testorg", "testrepo",
		"hooks", "post-worktree-add")

	target, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("readlink %s: %v", dst, err)
	}
	absSrc, _ := filepath.Abs(hookPath)
	if target != absSrc {
		t.Errorf("symlink target = %q; want %q", target, absSrc)
	}

	// Resolved file content must match what was committed (sanity check
	// that the symlink works end-to-end).
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read via symlink: %v", err)
	}
	if string(got) != hookBody {
		t.Errorf("content via symlink = %q; want %q", string(got), hookBody)
	}
}

// TestMirrorCommittedHooks_NonInteractivePromptDegrades verifies that when
// a clone runs in CI (no TTY, no Stdin), --hooks=prompt becomes a no-op
// rather than blocking forever waiting for input.
func TestMirrorCommittedHooks_NonInteractivePromptDegrades(t *testing.T) {
	tempDir := t.TempDir()
	dataHome := filepath.Join(tempDir, "data")
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)

	hooksDir := filepath.Join(tempDir, ".git-hop", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "post-worktree-add"),
		[]byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := hooks.MirrorCommittedHooks(afero.NewOsFs(), hooks.MirrorOpts{
		WorktreePath: tempDir,
		RepoID:       "github.com/testorg/testrepo",
		Mode:         hooks.ModePrompt,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Installed != 0 {
		t.Fatalf("non-interactive prompt should not install: %+v", res)
	}
	dst := filepath.Join(dataHome, "github.com", "testorg", "testrepo",
		"hooks", "post-worktree-add")
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("expected dst not to exist; got err=%v", err)
	}
}

// cloneRecorder captures the exact sequence of clone lifecycle events —
// hook dispatches interleaved with the committed-hook mirror — plus the
// path and branch each dispatch received.
type cloneRecorder struct {
	seq      []string
	paths    map[string]string
	branches map[string]string
}

func newCloneRecorder() *cloneRecorder {
	return &cloneRecorder{
		paths:    map[string]string{},
		branches: map[string]string{},
	}
}

func (c *cloneRecorder) record(event string) func(string, string, string) error {
	return func(path, repoID, branch string) error {
		c.seq = append(c.seq, event)
		c.paths[event] = path
		c.branches[event] = branch
		return nil
	}
}

func (c *cloneRecorder) dispatch() hop.HookDispatchOptions {
	return hop.HookDispatchOptions{
		PreClone:        c.record("pre-clone"),
		PostWorktreeAdd: c.record("post-worktree-add"),
		PostClone:       c.record("post-clone"),
	}
}

func (c *cloneRecorder) mirror() hop.HookMirrorOptions {
	return hop.HookMirrorOptions{
		Run: func(worktreePath, repoID string) error {
			c.seq = append(c.seq, "mirror")
			c.paths["mirror"] = worktreePath
			return nil
		},
	}
}

// runRecordedClone drives CloneWorktree against mocks with the recorder
// wired into both the mirror and dispatch slots, and returns the project
// root it cloned into.
func runRecordedClone(t *testing.T, c *cloneRecorder) string {
	t.Helper()
	t.Setenv("GIT_HOP_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	projectRoot := filepath.Join(t.TempDir(), "proj")
	err := hop.CloneWorktree(afero.NewMemMapFs(), mocks.NewMockGit(),
		"git@github.com:testorg/testrepo.git", projectRoot,
		true, false, c.mirror(), c.dispatch())
	if err != nil {
		t.Fatalf("CloneWorktree: %v", err)
	}
	return projectRoot
}

func assertSeq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event sequence = %v; want %v", got, want)
	}
}

// TestCloneDispatchesHooksInOrder pins the EXACT clone lifecycle
// sequence, mirror included. Set membership is not enough: the ordering
// is the behavior, and it is what regresses silently.
//
// Specifically, "mirror" must sit BETWEEN the initial worktree existing
// and post-worktree-add firing. That is what makes a repo-level hook
// committed in the cloned repo apply to the very worktree that carried
// it — mirror it into the hopspace first, then fire.
func TestCloneDispatchesHooksInOrder(t *testing.T) {
	c := newCloneRecorder()
	projectRoot := runRecordedClone(t, c)

	assertSeq(t, c.seq, []string{
		"pre-clone", "mirror", "post-worktree-add", "post-clone",
	})

	initialWorktree := filepath.Join(projectRoot, "hops", "main")

	// post-worktree-add must see the initial worktree, NOT the hub root.
	// Handing it the hub root would make every hook that inspects
	// GIT_HOP_WORKTREE_PATH look at a bare repo with no source tree.
	if got := c.paths["post-worktree-add"]; got != initialWorktree {
		t.Errorf("post-worktree-add path = %q; want initial worktree %q",
			got, initialWorktree)
	}
	if got := c.paths["post-worktree-add"]; got == projectRoot {
		t.Errorf("post-worktree-add received hub root %q; must be the initial worktree", got)
	}
	if got := c.paths["mirror"]; got != initialWorktree {
		t.Errorf("mirror path = %q; want %q", got, initialWorktree)
	}
	if got := c.paths["post-clone"]; got != initialWorktree {
		t.Errorf("post-clone path = %q; want %q", got, initialWorktree)
	}
	if got := c.branches["post-worktree-add"]; got != "main" {
		t.Errorf("post-worktree-add branch = %q; want %q", got, "main")
	}
}

// TestClonePreCloneAnchoredOnProjectRoot covers the deliberate decision
// about which path pre-clone receives.
//
// pre-clone fires before any worktree exists, and FindHookFile's
// parent-directory walk climbs all the way to the filesystem root. Handing
// the dispatch "" (or the caller's cwd) would let a stray
// .git-hop/hooks/pre-clone in ANY ancestor of wherever the user happens to
// be standing hijack the clone. Anchoring on the intended project root
// keeps resolution deterministic — the walk starts at a directory that
// does not yet exist, so pre-clone can only ever resolve at hopspace or
// global level, which is the intended reach.
func TestClonePreCloneAnchoredOnProjectRoot(t *testing.T) {
	c := newCloneRecorder()
	projectRoot := runRecordedClone(t, c)

	got := c.paths["pre-clone"]
	if got == "" {
		t.Fatal("pre-clone received an empty path: the parent-dir walk would " +
			"then start from the process cwd and match stray ancestor hooks")
	}
	if got != projectRoot {
		t.Errorf("pre-clone path = %q; want project root %q", got, projectRoot)
	}
	// The branch is unknown at pre-clone time (resolving it requires
	// talking to the remote), so the hook must see it empty rather than a
	// guessed value.
	if got := c.branches["pre-clone"]; got != "" {
		t.Errorf("pre-clone branch = %q; want empty", got)
	}
}

// TestClonePreCloneWalkCannotEscapeToAncestors is the concrete hazard
// fixture: a pre-clone hook planted in an ancestor of the *process cwd*
// but NOT an ancestor of the project root must never resolve. This is what
// would break if the dispatch passed "" instead of the project root.
func TestClonePreCloneWalkCannotEscapeToAncestors(t *testing.T) {
	fs := afero.NewMemMapFs()

	dataHome := filepath.Join(t.TempDir(), "data")
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	// A stray hook sitting in an unrelated tree. Under a "" (or cwd)
	// anchor this is exactly the kind of file the walk can reach.
	stray := filepath.Join("/stray", ".git-hop", "hooks", "pre-clone")
	if err := fs.MkdirAll(filepath.Dir(stray), 0755); err != nil {
		t.Fatalf("mkdir stray: %v", err)
	}
	if err := afero.WriteFile(fs, stray, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	runner := hooks.NewRunner(fs)
	repoID := "github.com/testorg/testrepo"

	// The path CloneWorktree actually passes: the intended project root,
	// which lives outside /stray. Nothing must resolve.
	projectRoot := "/projects/testrepo"
	if found := runner.FindHookFile("pre-clone", projectRoot, repoID); found != "" {
		t.Errorf("pre-clone resolved to %q from project root %q; expected no match",
			found, projectRoot)
	}

	// Demonstrate the hazard is real rather than hypothetical: anchoring
	// anywhere under /stray DOES reach it. This is why the anchor choice
	// matters and is asserted above.
	inStray := filepath.Join("/stray", "somewhere", "deep")
	if found := runner.FindHookFile("pre-clone", inStray, repoID); found != stray {
		t.Errorf("sanity: walk from %q found %q; want %q", inStray, found, stray)
	}
}

// TestClonePreCloneFailureAbortsClone verifies a non-zero pre-clone exit
// stops the clone before any filesystem work happens.
func TestClonePreCloneFailureAbortsClone(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	fs := afero.NewMemMapFs()
	var fired []string

	projectRoot := filepath.Join(t.TempDir(), "proj")
	err := hop.CloneWorktree(fs, mocks.NewMockGit(),
		"git@github.com:testorg/testrepo.git", projectRoot, true, false,
		hop.HookMirrorOptions{
			Run: func(string, string) error {
				fired = append(fired, "mirror")
				return nil
			},
		},
		hop.HookDispatchOptions{
			PreClone: func(string, string, string) error {
				fired = append(fired, "pre-clone")
				return os.ErrPermission
			},
			PostWorktreeAdd: func(string, string, string) error {
				fired = append(fired, "post-worktree-add")
				return nil
			},
			PostClone: func(string, string, string) error {
				fired = append(fired, "post-clone")
				return nil
			},
		})

	if err == nil {
		t.Fatal("expected clone to abort when pre-clone fails")
	}
	if !strings.Contains(err.Error(), "pre-clone") {
		t.Errorf("error = %v; want it to name the pre-clone hook", err)
	}
	assertSeq(t, fired, []string{"pre-clone"})

	// Nothing on disk: pre-clone runs before any filesystem work.
	if exists, _ := afero.DirExists(fs, projectRoot); exists {
		t.Errorf("project root %q created despite pre-clone abort", projectRoot)
	}
}
