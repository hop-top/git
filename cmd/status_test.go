package cmd

import (
	"errors"
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/test/mocks"
)

// TestComposePsHasRunning covers the empty cases for the output of
// `docker compose ps --format json`. Compose emits either an empty JSON
// array ("[]") or an empty string when no containers exist; a naive
// len(s) > 0 check would report true for "[]" and overcount.
func TestComposePsHasRunning(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string", "", false},
		{"whitespace only", "   \n", false},
		{"empty array", "[]", false},
		{"empty array with whitespace", "  [] \n", false},
		{"array with one entry", `[{"Name":"svc"}]`, true},
		{"array with multiple entries", `[{"Name":"a"},{"Name":"b"}]`, true},
		{"json lines single entry", `{"Name":"svc"}`, true},
		{"json lines multiple entries", `{"Name":"a"}
{"Name":"b"}`, true},
		{"json lines blank lines only", "\n\n\n", false},
		{"malformed non-empty (defensive true)", `garbage`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composePsHasRunning(tc.in)
			if got != tc.want {
				t.Fatalf("composePsHasRunning(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestGetBranchSyncStatus covers the branch position categories
// surfaced by `git hop status`: default, synced, merged, behind,
// ahead, diverged, and the unknown fallback. The mock returns
// rev-list --left-right --count output of the form "<ahead>\t<behind>".
//
// "merged" requires more than ahead==0. ahead==0 also matches unborn
// branches (created from default, never committed) and reset branches
// (`git reset --hard <default>` wiped them) — both produce ahead==0
// indistinguishable from a real merge-commit-merged branch. To avoid
// the false positive we gate `merged` on two additional local signals:
//   1. The local tracking ref `refs/remotes/origin/<branch>` is absent
//      (PR-merge with --delete-branch removes it via fetch+prune)
//   2. AND `branch.<name>.merge` config is present (= branch was once
//      tracking a remote, so the ref being gone means deletion, not
//      "never pushed")
// When either signal is missing, we drop the "merged" claim and report
// the honest position: `behind (N)`.
//
// When the worktree is dirty (tracked-but-uncommitted edits, staged
// changes, or untracked files), every commit-derived label is suffixed
// with a detailed dirty segment: `, dirty (T tracked +X/-Y, C unmerged,
// U untracked)`. Only nonzero segments are emitted; line counts are
// dropped when shortstat probes fail. `default` and `unknown` are left
// untouched: the default branch's dirtiness is surfaced elsewhere, and
// `unknown` already signals we couldn't probe.
func TestGetBranchSyncStatus(t *testing.T) {
	const (
		dir    = "/tmp/wt"
		def    = "main"
		branch = "feature"
		revKey = dir + ":git rev-list --left-right --count " + branch + "..." + def
		refKey = dir + ":git rev-parse --verify refs/remotes/origin/" + branch
		cfgKey = dir + ":git config --get branch." + branch + ".merge"
		// Diff shortstat probes for line counts in the dirty suffix.
		unstagedKey = dir + ":git diff --shortstat"
		stagedKey   = dir + ":git diff --cached --shortstat"
	)

	dirtyTracked := &git.Status{
		Branch: branch, Clean: false,
		Files: []string{"1 .M N... 100644 100644 100644 abc abc README.md"},
	}
	dirtyUntrackedOnly := &git.Status{
		Branch: branch, Clean: false,
		Files: []string{"? new.go", "? other.go"},
	}
	dirtyMixed := &git.Status{
		Branch: branch, Clean: false,
		Files: []string{
			"1 .M N... 100644 100644 100644 abc abc tracked.go",
			"u UU N... 100644 100644 100644 100644 a b c conflict.go",
			"? untracked.go",
		},
	}

	cases := []struct {
		name          string
		branch        string
		defaultBranch string
		response      string
		err           error
		status        *git.Status // nil → mock default (clean)
		// Remote-tracking-ref state for the "merged vs. behind" gate.
		originRefGone bool
		hadUpstream   bool
		// Shortstat outputs for the dirty-detail probes. Empty string means
		// "no diff output" (matches what git emits when nothing is changed
		// in that index). Use unstagedErr/stagedErr to simulate a failed
		// probe — the formatter must fall back to count-only.
		unstagedShortstat string
		stagedShortstat   string
		want              string
	}{
		// Existing position cases — no dirty suffix.
		{"default branch itself", "main", "main", "", nil, nil, false, false, "", "", "default"},
		{"empty default falls back", "feature", "", "", nil, nil, false, false, "", "", "default"},
		{"synced — same head", branch, def, "0\t0", nil, nil, false, false, "", "", "synced"},
		{"ahead only", branch, def, "5\t0", nil, nil, false, false, "", "", "5 ahead"},
		{"diverged", branch, def, "2\t4", nil, nil, false, false, "", "", "diverged (2 ahead, 4 behind)"},
		{"git error → unknown", branch, def, "", errors.New("boom"), nil, false, false, "", "", "unknown"},
		{"malformed output → unknown", branch, def, "garbage", nil, nil, false, false, "", "", "unknown"},

		// merged-vs-behind gate (ahead==0, behind>0).
		{"merged — remote deleted after tracking", branch, def, "0\t3", nil, nil, true, true, "", "", "merged (3 behind)"},
		{"behind — unborn (no remote, no upstream)", branch, def, "0\t3", nil, nil, true, false, "", "", "behind (3)"},
		{"behind — origin still present", branch, def, "0\t3", nil, nil, false, true, "", "", "behind (3)"},
		{"behind — ref gone but upstream config missing", branch, def, "0\t3", nil, nil, true, false, "", "", "behind (3)"},

		// Dirty detail — counts + line deltas from --shortstat.
		// Tracked-only with stats present.
		{
			"dirty: 1 tracked, +42/-7 unstaged",
			branch, def, "0\t0", nil, dirtyTracked, false, false,
			" 1 file changed, 42 insertions(+), 7 deletions(-)", "",
			"synced, dirty (1 tracked +42/-7)",
		},
		// Tracked across both staged and unstaged — sums.
		{
			"dirty: tracked spanning staged + unstaged",
			branch, def, "0\t0", nil, dirtyTracked, false, false,
			" 1 file changed, 10 insertions(+), 2 deletions(-)",
			" 1 file changed, 32 insertions(+), 5 deletions(-)",
			"synced, dirty (1 tracked +42/-7)",
		},
		// Untracked-only — no line stats segment.
		{
			"dirty: untracked only",
			branch, def, "0\t0", nil, dirtyUntrackedOnly, false, false,
			"", "",
			"synced, dirty (2 untracked)",
		},
		// Mixed tracked + unmerged + untracked, with stats.
		{
			"dirty: mixed segments",
			branch, def, "0\t3", nil, dirtyMixed, false, false,
			" 1 file changed, 8 insertions(+), 1 deletion(-)", "",
			"behind (3), dirty (1 tracked +8/-1, 1 unmerged, 1 untracked)",
		},
		// Stats probe fails (e.g. dubious-ownership) → tracked count only.
		{
			"dirty: tracked count only when shortstat fails",
			branch, def, "0\t0", nil, dirtyTracked, false, false,
			"", "", // both empty → no line deltas
			"synced, dirty (1 tracked)",
		},
		// Pure adds (no deletions in shortstat).
		{
			"dirty: tracked with insertions only",
			branch, def, "0\t0", nil, dirtyTracked, false, false,
			" 1 file changed, 5 insertions(+)", "",
			"synced, dirty (1 tracked +5/-0)",
		},
		// Pure deletes.
		{
			"dirty: tracked with deletions only",
			branch, def, "0\t0", nil, dirtyTracked, false, false,
			" 1 file changed, 3 deletions(-)", "",
			"synced, dirty (1 tracked +0/-3)",
		},
		// The originally reported bug, fully formatted.
		{
			"behind + dirty (the reported bug)",
			branch, def, "0\t3", nil, dirtyTracked, false, false,
			" 1 file changed, 12 insertions(+), 3 deletions(-)", "",
			"behind (3), dirty (1 tracked +12/-3)",
		},
		// Merged + dirty with deltas — confirms suffix composes with merge label.
		{
			"merged + dirty with deltas",
			branch, def, "0\t3", nil, dirtyTracked, true, true,
			" 1 file changed, 12 insertions(+), 3 deletions(-)", "",
			"merged (3 behind), dirty (1 tracked +12/-3)",
		},
		// Ahead + dirty.
		{
			"ahead + dirty",
			branch, def, "5\t0", nil, dirtyTracked, false, false,
			" 1 file changed, 2 insertions(+), 0 deletions(-)", "",
			"5 ahead, dirty (1 tracked +2/-0)",
		},
		// Diverged + dirty.
		{
			"diverged + dirty",
			branch, def, "2\t4", nil, dirtyMixed, false, false,
			" 1 file changed, 8 insertions(+), 1 deletion(-)", "",
			"diverged (2 ahead, 4 behind), dirty (1 tracked +8/-1, 1 unmerged, 1 untracked)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			if tc.err != nil {
				m.Runner.Errors[revKey] = tc.err
			} else {
				m.Runner.Responses[revKey] = tc.response
			}
			if tc.originRefGone {
				m.Runner.Errors[refKey] = errors.New("ref not found")
			} else {
				m.Runner.Responses[refKey] = "abc123"
			}
			if tc.hadUpstream {
				m.Runner.Responses[cfgKey] = "refs/heads/" + tc.branch
			} else {
				m.Runner.Errors[cfgKey] = errors.New("not set")
			}
			m.Runner.Responses[unstagedKey] = tc.unstagedShortstat
			m.Runner.Responses[stagedKey] = tc.stagedShortstat
			if tc.status != nil {
				m.StatusOverride = tc.status
			}

			got := getBranchSyncStatus(m, dir, tc.branch, tc.defaultBranch)
			if got != tc.want {
				t.Fatalf("getBranchSyncStatus(%q, %q) = %q, want %q",
					tc.branch, tc.defaultBranch, got, tc.want)
			}
		})
	}
}
