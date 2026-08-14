package hop_test

import (
	"testing"

	"hop.top/git/internal/hop"
	"hop.top/git/test/mocks"
)

// porcelainFixture mirrors the exact shape `git worktree list --porcelain`
// emits for a bare hub with one attached branch worktree and one detached
// worktree. Attached entries carry BOTH a `HEAD <sha>` line and a
// `branch refs/heads/<name>` line; detached entries carry `HEAD <sha>` +
// `detached`. The bare root carries neither.
const porcelainFixture = "" +
	"worktree /hub\n" +
	"bare\n" +
	"\n" +
	"worktree /hub/hops/main\n" +
	"HEAD 9ae2abc60284f0a62dd46aceda2dd0a361a83d2f\n" +
	"branch refs/heads/main\n" +
	"\n" +
	"worktree /hub/hops/feat/x\n" +
	"HEAD 1111111111111111111111111111111111111111\n" +
	"branch refs/heads/feat/x\n" +
	"\n" +
	"worktree /hub/hops/loose\n" +
	"HEAD 2222222222222222222222222222222222222222\n" +
	"detached\n"

func newPorcelainMock(dir string) *mocks.MockGit {
	return &mocks.MockGit{
		Runner: &mocks.MockCommandRunner{
			Responses: map[string]string{
				dir + ":git worktree list --porcelain": porcelainFixture,
			},
		},
	}
}

// TestListWorktrees_ReturnsBranchNamesNotSHAs pins that ListWorktrees reads
// the `branch refs/heads/<name>` line. It previously read the `HEAD <sha>`
// line, so every entry came back as a 40-char commit SHA. `git hop init`
// fed those straight into the worktree directory name, producing
// hops/<sha>/ instead of hops/<branch>/ while hop.json and the `current`
// symlink recorded hops/<default-branch> — a dangling symlink and a
// hop.json path that did not exist.
func TestListWorktrees_ReturnsBranchNamesNotSHAs(t *testing.T) {
	g := newPorcelainMock("/hub")

	got, err := hop.ListWorktrees(g, "/hub")
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}

	want := []string{"main", "feat/x"}
	if len(got) != len(want) {
		t.Fatalf("ListWorktrees() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListWorktrees()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestListWorktrees_SkipsDetachedWorktrees verifies a detached HEAD entry
// contributes no name at all — there is no branch to derive a directory
// name from, so it must not be materialized as a worktree.
func TestListWorktrees_SkipsDetachedWorktrees(t *testing.T) {
	g := newPorcelainMock("/hub")

	got, err := hop.ListWorktrees(g, "/hub")
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}

	for _, name := range got {
		if len(name) == 40 {
			t.Errorf("ListWorktrees returned what looks like a commit SHA: %q", name)
		}
		if name == "loose" {
			t.Errorf("ListWorktrees returned detached worktree entry %q", name)
		}
	}
}

// TestGetWorktreePath_MatchesOnBranchName pins the sibling lookup: it must
// match against the `branch refs/heads/<name>` line, not the SHA.
func TestGetWorktreePath_MatchesOnBranchName(t *testing.T) {
	g := newPorcelainMock("/hub")

	got, err := hop.GetWorktreePath(g, "/hub", "feat/x")
	if err != nil {
		t.Fatalf("GetWorktreePath: %v", err)
	}
	if want := "/hub/hops/feat/x"; got != want {
		t.Errorf("GetWorktreePath = %q, want %q", got, want)
	}
}
