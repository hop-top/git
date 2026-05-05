package hop

import (
	"errors"
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/test/mocks"
	"github.com/spf13/afero"
)

// stubGit wraps mocks.MockGit and overrides RevParse + RunInDir so
// resolveStartPoint can probe specific refs/SHAs deterministically.
type stubGit struct {
	*mocks.MockGit
	// RevParseRefs lists refs (as passed verbatim to --verify) that should
	// resolve. Anything not listed returns an error to mimic a missing ref.
	RevParseRefs map[string]bool
	// RootCommitOut is the trimmed multi-line output of
	// `git rev-list --max-parents=0 HEAD`. Tests use this to drive
	// the "initial" code path. RootCommitErr injects a failure.
	RootCommitOut string
	RootCommitErr error
}

func newStubGit() *stubGit {
	return &stubGit{
		MockGit:      mocks.NewMockGit(),
		RevParseRefs: map[string]bool{},
	}
}

func (s *stubGit) RevParse(dir string, args ...string) (string, error) {
	// Match worktree.resolveStartPoint's call shape:
	// RevParse(basePath, "--verify", "<ref>")
	if len(args) == 2 && args[0] == "--verify" {
		if s.RevParseRefs[args[1]] {
			return "abcdef0123456789", nil
		}
		return "", errors.New("ref not found")
	}
	return "", nil
}

func (s *stubGit) RunInDir(dir, cmd string, args ...string) (string, error) {
	if cmd == "git" && len(args) >= 1 && args[0] == "rev-list" {
		return s.RootCommitOut, s.RootCommitErr
	}
	return s.MockGit.RunInDir(dir, cmd, args...)
}

// Compile-time check that stubGit still satisfies the Git interface.
var _ git.GitInterface = (*stubGit)(nil)

func newWM(g git.GitInterface) *WorktreeManager {
	return NewWorktreeManager(afero.NewMemMapFs(), g)
}

func TestResolveStartPoint_EmptyPrefersOriginRef(t *testing.T) {
	g := newStubGit()
	g.RevParseRefs["refs/remotes/origin/main"] = true
	g.RevParseRefs["refs/heads/main"] = true

	resolved, suppress := newWM(g).resolveStartPoint("/base", "", "main")
	if resolved != "refs/remotes/origin/main" {
		t.Errorf("resolved = %q, want refs/remotes/origin/main", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false, want true (origin ref is the start point)")
	}
}

func TestResolveStartPoint_DefaultBranchSentinelMatchesEmpty(t *testing.T) {
	g := newStubGit()
	g.RevParseRefs["refs/remotes/origin/main"] = true

	resolved, suppress := newWM(g).resolveStartPoint("/base", StartPointDefaultBranch, "main")
	if resolved != "refs/remotes/origin/main" {
		t.Errorf("resolved = %q, want refs/remotes/origin/main", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false, want true")
	}
}

func TestResolveStartPoint_FallsBackToLocalHeads(t *testing.T) {
	g := newStubGit()
	// origin missing; local heads/main present.
	g.RevParseRefs["refs/heads/main"] = true

	resolved, suppress := newWM(g).resolveStartPoint("/base", "", "main")
	if resolved != "refs/heads/main" {
		t.Errorf("resolved = %q, want refs/heads/main", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false, want true")
	}
}

func TestResolveStartPoint_NeitherRefFallsBackToHEAD(t *testing.T) {
	g := newStubGit() // no refs registered

	resolved, suppress := newWM(g).resolveStartPoint("/base", "", "main")
	if resolved != "HEAD" {
		t.Errorf("resolved = %q, want HEAD", resolved)
	}
	if suppress {
		t.Error("suppressTrack = true, want false (HEAD fallback should let trackBranch through)")
	}
}

func TestResolveStartPoint_InitialResolvesRootCommit(t *testing.T) {
	g := newStubGit()
	// rev-list output: newest root first, oldest root last; we want last line.
	g.RootCommitOut = "deadbeef\nfeedface"

	resolved, suppress := newWM(g).resolveStartPoint("/base", StartPointInitial, "main")
	if resolved != "feedface" {
		t.Errorf("resolved = %q, want feedface (last line of rev-list)", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false, want true (explicit start-point)")
	}
}

func TestResolveStartPoint_InitialFailureFallsBackToHEAD(t *testing.T) {
	g := newStubGit()
	g.RootCommitErr = errors.New("boom")

	resolved, suppress := newWM(g).resolveStartPoint("/base", StartPointInitial, "main")
	if resolved != "HEAD" {
		t.Errorf("resolved = %q, want HEAD", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false; we still treat 'initial' as explicit even on failure")
	}
}

func TestResolveStartPoint_ExplicitSHAPassesThrough(t *testing.T) {
	g := newStubGit()

	resolved, suppress := newWM(g).resolveStartPoint("/base", "abc1234", "main")
	if resolved != "abc1234" {
		t.Errorf("resolved = %q, want abc1234", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false, want true (explicit ref)")
	}
}

func TestResolveStartPoint_ExplicitOriginRefPassesThrough(t *testing.T) {
	g := newStubGit()

	resolved, suppress := newWM(g).resolveStartPoint("/base", "origin/develop", "main")
	if resolved != "origin/develop" {
		t.Errorf("resolved = %q, want origin/develop", resolved)
	}
	if !suppress {
		t.Error("suppressTrack = false, want true (explicit ref)")
	}
}

func TestResolveStartPoint_EmptyDefaultBranchFallsBackToHEAD(t *testing.T) {
	g := newStubGit()

	resolved, suppress := newWM(g).resolveStartPoint("/base", "", "")
	if resolved != "HEAD" {
		t.Errorf("resolved = %q, want HEAD", resolved)
	}
	if suppress {
		t.Error("suppressTrack = true, want false")
	}
}
