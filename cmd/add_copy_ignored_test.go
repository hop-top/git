package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// newTestHub builds a hub whose registered branches all have on-disk
// worktree directories, so resolveCopySource's existence check is real.
func newTestHub(t *testing.T, fs afero.Fs, defaultBranch string, branches ...string) *hop.Hub {
	t.Helper()
	hubPath := "/hub"
	cfg := &config.HubConfig{
		Branches: map[string]config.HubBranch{},
	}
	cfg.Repo.DefaultBranch = defaultBranch

	for _, b := range branches {
		rel := filepath.Join("hops", b)
		cfg.Branches[b] = config.HubBranch{Path: rel}
		if err := fs.MkdirAll(filepath.Join(hubPath, rel), 0o755); err != nil {
			t.Fatalf("mkdir worktree for %s: %v", b, err)
		}
	}
	return &hop.Hub{Path: hubPath, Config: cfg}
}

func TestResolveCopySource_DefaultStartPointUsesDefaultBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := newTestHub(t, fs, "main", "main", "other")

	for _, sp := range []string{"", hop.StartPointDefaultBranch} {
		got := resolveCopySource(fs, hub, sp)
		want := filepath.Join("/hub", "hops", "main")
		if got != want {
			t.Errorf("startPoint %q: got %q, want %q", sp, got, want)
		}
	}
}

// --from <branch> means the new worktree forks from that branch, so its
// worktree is the one holding the ignored state the user wants carried over.
func TestResolveCopySource_ExplicitStartPointWins(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := newTestHub(t, fs, "main", "main", "feature-x")

	got := resolveCopySource(fs, hub, "feature-x")
	want := filepath.Join("/hub", "hops", "feature-x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A start-point naming a branch with no worktree (or a raw SHA / tag) is
// not itself a source, but the default branch still is — that is where the
// user's local state lives.
func TestResolveCopySource_UncheckedOutStartPointFallsBackToDefault(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := newTestHub(t, fs, "main", "main")

	for _, sp := range []string{"deadbeef", "v1.2.3", "branch-without-worktree"} {
		got := resolveCopySource(fs, hub, sp)
		want := filepath.Join("/hub", "hops", "main")
		if got != want {
			t.Errorf("startPoint %q: got %q, want %q", sp, got, want)
		}
	}
}

// The root commit belongs to no branch; the default branch's worktree is
// still the sensible source for local state.
func TestResolveCopySource_InitialUsesDefaultBranch(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := newTestHub(t, fs, "main", "main")

	got := resolveCopySource(fs, hub, hop.StartPointInitial)
	if want := filepath.Join("/hub", "hops", "main"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// When nothing resolves, the answer is "" — the caller must skip and say
// so rather than guess at an unrelated worktree and copy a stranger's .env.
func TestResolveCopySource_UnresolvableReturnsEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := newTestHub(t, fs, "main") // registered nothing

	if got := resolveCopySource(fs, hub, "whatever"); got != "" {
		t.Errorf("got %q, want empty so the caller skips with a message", got)
	}
}

// A branch registered in hop.json whose directory is gone must not be
// offered as a source.
func TestResolveCopySource_MissingDirectoryNotUsed(t *testing.T) {
	fs := afero.NewMemMapFs()
	hub := newTestHub(t, fs, "main", "main")
	// Register a branch but never create its directory.
	hub.Config.Branches["ghost"] = config.HubBranch{Path: filepath.Join("hops", "ghost")}

	got := resolveCopySource(fs, hub, "ghost")
	if want := filepath.Join("/hub", "hops", "main"); got != want {
		t.Errorf("got %q, want fallback to %q", got, want)
	}
}

// --- flag plumbing ---

func newCopyIgnoredFlagCmd() *cobra.Command {
	c := &cobra.Command{Use: "add"}
	var yes, no bool
	c.Flags().BoolVar(&yes, "copy-ignored", true, "")
	c.Flags().BoolVar(&no, "no-copy-ignored", false, "")
	return c
}

func TestCopyIgnoredOverride(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want *bool
	}{
		{"neither flag defers to config", nil, nil},
		{"--copy-ignored forces on", []string{"--copy-ignored"}, boolPtr(true)},
		{"--copy-ignored=false forces off", []string{"--copy-ignored=false"}, boolPtr(false)},
		{"--no-copy-ignored forces off", []string{"--no-copy-ignored"}, boolPtr(false)},
		{"--no-copy-ignored wins over --copy-ignored",
			[]string{"--copy-ignored", "--no-copy-ignored"}, boolPtr(false)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newCopyIgnoredFlagCmd()
			if err := c.Flags().Parse(tc.args); err != nil {
				t.Fatalf("parse %v: %v", tc.args, err)
			}
			yes, _ := c.Flags().GetBool("copy-ignored")
			no, _ := c.Flags().GetBool("no-copy-ignored")

			got := copyIgnoredOverride(c, yes, no)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got %v, want nil (config decides)", *got)
			case tc.want != nil && got == nil:
				t.Errorf("got nil, want %v", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("got %v, want %v", *got, *tc.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// The flags must actually be registered on the real command, not just in
// the test's stand-in.
func TestAddCmd_RegistersCopyIgnoredFlags(t *testing.T) {
	for _, name := range []string{"copy-ignored", "no-copy-ignored"} {
		if addCmd.Flags().Lookup(name) == nil {
			t.Errorf("add command is missing the --%s flag", name)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{512, "512B"},
		{2048, "2.0K"},
		{10 << 20, "10.0M"},
		{3 << 30, "3.0G"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
