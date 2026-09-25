package cmd

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/test/mocks"
)

// TestRemoveGate covers the safety cases plus their flag-bypass
// combinations. The matrix mirrors the documented spec on `git hop
// remove`: --force answers the not-merged check, --no-verify answers
// dirty or unpushed state, so unmerged+unpushed and unmerged+dirty need
// both, unmerged+pushed+clean needs --force, merged+dirty needs
// --no-verify, and merged+clean is silent.
func TestRemoveGate(t *testing.T) {
	cases := []struct {
		name      string
		safety    branchSafety
		force     bool
		noVerify  bool
		wantErr   bool
		errSubstr string
	}{
		// Case 1: unmerged + unpushed.
		{"unmerged unpushed: no flags", branchSafety{}, false, false, true, "--force --no-verify"},
		{"unmerged unpushed: only force", branchSafety{}, true, false, true, "--force --no-verify"},
		{"unmerged unpushed: only no-verify", branchSafety{}, false, true, true, "--force --no-verify"},
		{"unmerged unpushed: both flags", branchSafety{}, true, true, false, ""},

		// Case 2: unmerged + pushed.
		{"unmerged pushed: no flags", branchSafety{Pushed: true, Clean: true}, false, false, true, "not merged"},
		{"unmerged pushed: only no-verify", branchSafety{Pushed: true, Clean: true}, false, true, true, "not merged"},
		{"unmerged pushed: only force", branchSafety{Pushed: true, Clean: true}, true, false, false, ""},
		{"unmerged pushed: both flags", branchSafety{Pushed: true, Clean: true}, true, true, false, ""},

		// Case 2b: unmerged + pushed + dirty. Being on origin protects
		// the commits, not the uncommitted or untracked files, so
		// --force alone must not discard them.
		{"unmerged pushed dirty: no flags", branchSafety{Pushed: true}, false, false, true, "--force --no-verify"},
		{"unmerged pushed dirty: only force", branchSafety{Pushed: true}, true, false, true, "--force --no-verify"},
		{"unmerged pushed dirty: only force names dirty state", branchSafety{Pushed: true}, true, false, true, "uncommitted changes or untracked files"},
		{"unmerged pushed dirty: only no-verify", branchSafety{Pushed: true}, false, true, true, "--force --no-verify"},
		{"unmerged pushed dirty: only no-verify names not-merged", branchSafety{Pushed: true}, false, true, true, "not merged"},
		{"unmerged pushed dirty: both flags", branchSafety{Pushed: true}, true, true, false, ""},

		// Case 3: merged + dirty (Pushed irrelevant).
		{"merged dirty: no flags", branchSafety{Merged: true}, false, false, true, "uncommitted"},
		{"merged dirty: only force", branchSafety{Merged: true}, true, false, true, "uncommitted"},
		{"merged dirty: only no-verify", branchSafety{Merged: true}, false, true, false, ""},
		{"merged dirty pushed: only no-verify", branchSafety{Merged: true, Pushed: true}, false, true, false, ""},

		// Case 4: merged + clean — silent pass with or without flags.
		{"merged clean: no flags", branchSafety{Merged: true, Clean: true}, false, false, false, ""},
		{"merged clean pushed: no flags", branchSafety{Merged: true, Pushed: true, Clean: true}, false, false, false, ""},
		{"merged clean: extra flags", branchSafety{Merged: true, Clean: true}, true, true, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := removeGate(tc.safety, tc.force, tc.noVerify)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
			}
		})
	}
}

// TestRemoveGateHintIsSelfSufficient pins the contract that following a
// gate hint verbatim succeeds on the first retry. Every hint must name
// the complete non-interactive flag set — including --no-prompt, which
// the gate itself does not require but the confirmation prompt that
// runs immediately afterwards does. Omitting it sent scripted callers
// into a second guaranteed failure.
func TestRemoveGateHintIsSelfSufficient(t *testing.T) {
	cases := []struct {
		name      string
		safety    branchSafety
		wantFlags []string
	}{
		{"unmerged unpushed", branchSafety{}, []string{"--force", "--no-verify", "--no-prompt"}},
		{"unmerged pushed", branchSafety{Pushed: true, Clean: true}, []string{"--force", "--no-prompt"}},
		{"unmerged pushed dirty", branchSafety{Pushed: true}, []string{"--force", "--no-verify", "--no-prompt"}},
		{"merged dirty", branchSafety{Merged: true}, []string{"--no-verify", "--no-prompt"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := removeGate(tc.safety, false, false)
			if err == nil {
				t.Fatalf("expected gate error for %+v", tc.safety)
			}
			for _, f := range tc.wantFlags {
				if !strings.Contains(err.Error(), f) {
					t.Fatalf("hint %q does not name %q", err.Error(), f)
				}
			}

			// Re-running with exactly the flags the hint named must
			// satisfy the gate — otherwise the hint is still a dead end.
			force := slices.Contains(tc.wantFlags, "--force")
			noVerify := slices.Contains(tc.wantFlags, "--no-verify")
			if err := removeGate(tc.safety, force, noVerify); err != nil {
				t.Fatalf("following the hint still fails the gate: %v", err)
			}
		})
	}
}

// TestInspectBranchSafety verifies the probe wires the right git
// commands and interprets their output correctly. The mock keys
// responses by "<dir>:git <args>" so the test pins the exact commands
// the gate relies on.
func TestInspectBranchSafety(t *testing.T) {
	const (
		dir    = "/wt"
		branch = "feature"
		def    = "main"
	)

	// Command keys used by inspectBranchSafety, in the order it calls
	// them. The MockCommandRunner builds keys as "<dir>:<cmd> <args>".
	mergedKey := dir + ":git rev-list --count " + branch + " --not " + def
	verifyOriginKey := dir + ":git rev-parse --verify refs/remotes/origin/" + branch
	pushedKey := dir + ":git rev-list --count " + branch + " --not refs/remotes/origin/" + branch

	cleanStatus := &git.Status{Branch: branch, Clean: true}
	dirtyStatus := &git.Status{Branch: branch, Clean: false, Files: []string{"? untracked.txt"}}

	cases := []struct {
		name      string
		responses map[string]string
		errs      map[string]error
		status    *git.Status
		want      branchSafety
	}{
		{
			name: "fully merged, fully pushed, clean",
			responses: map[string]string{
				mergedKey:       "0",
				verifyOriginKey: "deadbeef",
				pushedKey:       "0",
			},
			status: cleanStatus,
			want:   branchSafety{Merged: true, Pushed: true, Clean: true},
		},
		{
			name: "ahead of default and remote, clean",
			responses: map[string]string{
				mergedKey:       "3",
				verifyOriginKey: "deadbeef",
				pushedKey:       "2",
			},
			status: cleanStatus,
			want:   branchSafety{Clean: true},
		},
		{
			name: "no remote tracking branch",
			responses: map[string]string{
				mergedKey: "1",
			},
			errs: map[string]error{
				verifyOriginKey: errors.New("unknown ref"),
			},
			status: cleanStatus,
			want:   branchSafety{Clean: true},
		},
		{
			name: "merged but dirty",
			responses: map[string]string{
				mergedKey:       "0",
				verifyOriginKey: "deadbeef",
				pushedKey:       "0",
			},
			status: dirtyStatus,
			want:   branchSafety{Merged: true, Pushed: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			m.StatusOverride = tc.status
			for k, v := range tc.responses {
				m.Runner.Responses[k] = v
			}
			for k, e := range tc.errs {
				m.Runner.Errors[k] = e
			}

			got := inspectBranchSafety(m, dir, branch, def)
			if got != tc.want {
				t.Fatalf("inspectBranchSafety = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestInspectBranchSafety_DefaultBranchSelf documents that the probe
// reports Merged=false when called for the default branch itself —
// remove.go has a separate guard preventing default-branch removal,
// so the gate never sees this case in practice.
func TestInspectBranchSafety_DefaultBranchSelf(t *testing.T) {
	m := mocks.NewMockGit()
	got := inspectBranchSafety(m, "/wt", "main", "main")
	if got.Merged {
		t.Fatalf("expected Merged=false for branch==default, got %+v", got)
	}
}

// contentProbeKey builds the mock key for the content-equivalence probe
// issued by branchContentMergedInto.
func contentProbeKey(dir, branch, defaultBranch string) string {
	return dir + ":git merge-tree --write-tree " + defaultBranch + " " + branch
}

// defaultTreeKey builds the mock key for the default branch's tree OID
// lookup that the content probe compares against.
func defaultTreeKey(dir, defaultBranch string) string {
	return dir + ":git rev-parse " + defaultBranch + "^{tree}"
}

// TestBranchContentMergedInto pins the content-equivalence probe in
// isolation. The probe answers "would merging <branch> into <default>
// change <default>?" by merging the two in memory and comparing the
// resulting tree against the default branch's current tree. Equal trees
// mean the branch contributes nothing default does not already have —
// which is exactly the shape a squash- or rebase-merge leaves behind.
//
// Every error path must resolve to false. A false positive here causes
// `git hop remove --merged` to delete unmerged work, so uncertainty
// (missing ref, git too old to support --write-tree, unreadable tree)
// must never read as "merged".
func TestBranchContentMergedInto(t *testing.T) {
	const (
		dir    = "/wt"
		branch = "feature"
		def    = "main"
	)
	mergeKey := contentProbeKey(dir, branch, def)
	treeKey := defaultTreeKey(dir, def)

	cases := []struct {
		name      string
		responses map[string]string
		errs      map[string]error
		want      bool
	}{
		{
			name: "merged tree equals default tree: content already in default",
			responses: map[string]string{
				mergeKey: "aaa111",
				treeKey:  "aaa111",
			},
			want: true,
		},
		{
			name: "merged tree differs: branch adds content default lacks",
			responses: map[string]string{
				mergeKey: "bbb222",
				treeKey:  "aaa111",
			},
			want: false,
		},
		{
			name: "merge-tree fails (missing default ref): fail closed",
			responses: map[string]string{
				treeKey: "aaa111",
			},
			errs: map[string]error{
				mergeKey: errors.New("not something we can merge"),
			},
			want: false,
		},
		{
			name: "merge-tree unsupported by old git: fail closed",
			responses: map[string]string{
				treeKey: "aaa111",
			},
			errs: map[string]error{
				mergeKey: errors.New("unknown option `write-tree'"),
			},
			want: false,
		},
		{
			name: "default tree lookup fails: fail closed",
			responses: map[string]string{
				mergeKey: "aaa111",
			},
			errs: map[string]error{
				treeKey: errors.New("unknown revision"),
			},
			want: false,
		},
		{
			name: "empty merge-tree output: fail closed",
			responses: map[string]string{
				mergeKey: "   ",
				treeKey:  "   ",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			for k, v := range tc.responses {
				m.Runner.Responses[k] = v
			}
			for k, e := range tc.errs {
				m.Runner.Errors[k] = e
			}

			if got := branchContentMergedInto(m, dir, branch, def); got != tc.want {
				t.Fatalf("branchContentMergedInto = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInspectBranchSafety_ContentEquivalenceFallback covers the probe
// ordering: topology (rev-list) first, content-equivalence only as a
// fallback when topology says "ahead". The "never called" cases are
// asserted against recorded calls, not just the resulting boolean —
// a boolean alone cannot distinguish "skipped the probe" from "ran the
// probe and got false".
func TestInspectBranchSafety_ContentEquivalenceFallback(t *testing.T) {
	const (
		dir    = "/wt"
		branch = "feature"
		def    = "main"
	)
	mergedKey := dir + ":git rev-list --count " + branch + " --not " + def
	verifyOriginKey := dir + ":git rev-parse --verify refs/remotes/origin/" + branch
	mergeKey := contentProbeKey(dir, branch, def)
	treeKey := defaultTreeKey(dir, def)

	t.Run("topology merged: content probe never runs", func(t *testing.T) {
		m := mocks.NewMockGit()
		m.Runner.Responses[mergedKey] = "0"
		m.Runner.Errors[verifyOriginKey] = errors.New("unknown ref")
		// Wire the content probe to claim "merged" so that if it were
		// consulted the assertion below would still pass — only the call
		// record can prove it was skipped.
		m.Runner.Responses[mergeKey] = "aaa111"
		m.Runner.Responses[treeKey] = "aaa111"

		got := inspectBranchSafety(m, dir, branch, def)
		if !got.Merged {
			t.Fatalf("expected Merged=true from topology, got %+v", got)
		}
		if m.Runner.CalledWith(mergeKey) {
			t.Fatalf("content probe ran despite topology already proving merged; calls=%v", m.Runner.Calls)
		}
	})

	t.Run("topology unmerged, content equivalent: merged", func(t *testing.T) {
		m := mocks.NewMockGit()
		m.Runner.Responses[mergedKey] = "3"
		m.Runner.Errors[verifyOriginKey] = errors.New("unknown ref")
		m.Runner.Responses[mergeKey] = "aaa111"
		m.Runner.Responses[treeKey] = "aaa111"

		got := inspectBranchSafety(m, dir, branch, def)
		if !got.Merged {
			t.Fatalf("expected Merged=true from content equivalence, got %+v", got)
		}
		if !m.Runner.CalledWith(mergeKey) {
			t.Fatalf("content probe never ran; calls=%v", m.Runner.Calls)
		}
	})

	t.Run("topology unmerged, content differs: not merged", func(t *testing.T) {
		m := mocks.NewMockGit()
		m.Runner.Responses[mergedKey] = "3"
		m.Runner.Errors[verifyOriginKey] = errors.New("unknown ref")
		m.Runner.Responses[mergeKey] = "bbb222"
		m.Runner.Responses[treeKey] = "aaa111"

		got := inspectBranchSafety(m, dir, branch, def)
		if got.Merged {
			t.Fatalf("expected Merged=false when branch adds content, got %+v", got)
		}
	})

	t.Run("topology unmerged, content probe errors: fail closed", func(t *testing.T) {
		m := mocks.NewMockGit()
		m.Runner.Responses[mergedKey] = "3"
		m.Runner.Errors[verifyOriginKey] = errors.New("unknown ref")
		m.Runner.Errors[mergeKey] = errors.New("not something we can merge")
		m.Runner.Responses[treeKey] = "aaa111"

		got := inspectBranchSafety(m, dir, branch, def)
		if got.Merged {
			t.Fatalf("expected Merged=false when the content probe errors, got %+v", got)
		}
	})

	t.Run("empty default branch: no merge probes at all", func(t *testing.T) {
		m := mocks.NewMockGit()
		m.Runner.Responses[mergeKey] = "aaa111"
		m.Runner.Responses[treeKey] = "aaa111"

		got := inspectBranchSafety(m, dir, branch, "")
		if got.Merged {
			t.Fatalf("expected Merged=false with no default branch, got %+v", got)
		}
		if m.Runner.CalledWith(mergeKey) {
			t.Fatalf("content probe ran with an empty default branch; calls=%v", m.Runner.Calls)
		}
	})

	t.Run("branch is the default branch: no merge probes at all", func(t *testing.T) {
		m := mocks.NewMockGit()
		selfMergeKey := contentProbeKey(dir, def, def)
		m.Runner.Responses[selfMergeKey] = "aaa111"
		m.Runner.Responses[treeKey] = "aaa111"

		got := inspectBranchSafety(m, dir, def, def)
		if got.Merged {
			t.Fatalf("expected Merged=false for branch==default, got %+v", got)
		}
		if m.Runner.CalledWith(selfMergeKey) {
			t.Fatalf("content probe ran for branch==default; calls=%v", m.Runner.Calls)
		}
	})
}

// cherryKey builds the mock key for the patch-equivalence probe issued by
// branchPatchesInDefault.
func cherryKey(dir, branch, defaultBranch string) string {
	return dir + ":git cherry " + defaultBranch + " " + branch
}

// TestBranchPatchesInDefault pins the patch-equivalence probe: a branch
// counts as landed only when git cherry lists at least one commit and
// marks every one '-' (an equivalent change is on default). Anything
// else, including an error or empty output, fails closed.
func TestBranchPatchesInDefault(t *testing.T) {
	const (
		dir    = "/wt"
		branch = "feature"
		def    = "main"
	)
	key := cherryKey(dir, branch, def)

	cases := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{"every commit patch-equivalent", "- aaa\n- bbb\n", nil, true},
		{"one commit not on default", "- aaa\n+ bbb\n", nil, false},
		{"no commit on default", "+ aaa\n", nil, false},
		{"no commits listed: fail closed", "", nil, false},
		{"whitespace only: fail closed", "  \n", nil, false},
		{"cherry fails: fail closed", "", errors.New("unknown commit main"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			m.Runner.Responses[key] = tc.out
			if tc.err != nil {
				m.Runner.Errors[key] = tc.err
			}
			if got := branchPatchesInDefault(m, dir, branch, def); got != tc.want {
				t.Fatalf("branchPatchesInDefault(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

// TestInspectBranchSafety_PatchEquivalenceFallback covers a rebase-merged
// branch whose lines main changed again later: topology says ahead and
// the in-memory merge differs from main, but git cherry shows every
// commit on main. That is merged; one '+' commit is not.
func TestInspectBranchSafety_PatchEquivalenceFallback(t *testing.T) {
	const (
		dir    = "/wt"
		branch = "feature"
		def    = "main"
	)
	mergedKey := dir + ":git rev-list --count " + branch + " --not " + def
	verifyOriginKey := dir + ":git rev-parse --verify refs/remotes/origin/" + branch

	for _, tc := range []struct {
		name   string
		cherry string
		want   bool
	}{
		{"all commits patch-equivalent: merged", "- aaa\n- bbb\n", true},
		{"one commit unshipped: not merged", "- aaa\n+ bbb\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mocks.NewMockGit()
			m.Runner.Responses[mergedKey] = "2"
			m.Runner.Errors[verifyOriginKey] = errors.New("unknown ref")
			m.Runner.Responses[contentProbeKey(dir, branch, def)] = "bbb222"
			m.Runner.Responses[defaultTreeKey(dir, def)] = "aaa111"
			m.Runner.Responses[cherryKey(dir, branch, def)] = tc.cherry

			if got := inspectBranchSafety(m, dir, branch, def); got.Merged != tc.want {
				t.Fatalf("Merged = %v, want %v (%+v)", got.Merged, tc.want, got)
			}
		})
	}
}
