package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// writeBareConfig writes a minimal git bare-repo config file at <root>/config.
func writeBareConfig(t *testing.T, fs afero.Fs, root string) {
	t.Helper()
	content := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = true\n"
	if err := afero.WriteFile(fs, filepath.Join(root, "config"), []byte(content), 0644); err != nil {
		t.Fatalf("write bare config: %v", err)
	}
}

func TestDetectUnregisteredBareWorktreeRepo(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(fs afero.Fs)
		startPath string
		wantRoot  string
		wantOK    bool
	}{
		{
			name: "bare repo with hops/ and no hop.json — detected from root",
			setup: func(fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				_ = fs.MkdirAll("/repo/hops/main", 0755)
			},
			startPath: "/repo",
			wantRoot:  "/repo",
			wantOK:    true,
		},
		{
			name: "bare repo with hops/ and no hop.json — detected from inside hops/main",
			setup: func(fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				_ = fs.MkdirAll("/repo/hops/main", 0755)
			},
			startPath: "/repo/hops/main",
			wantRoot:  "/repo",
			wantOK:    true,
		},
		{
			name: "bare repo with hops/ and no hop.json — detected from nested branch path",
			setup: func(fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				_ = fs.MkdirAll("/repo/hops/feat/x", 0755)
			},
			startPath: "/repo/hops/feat/x",
			wantRoot:  "/repo",
			wantOK:    true,
		},
		{
			name: "bare repo with hops/ AND hop.json — already registered, NOT detected",
			setup: func(fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				_ = fs.MkdirAll("/repo/hops/main", 0755)
				_ = afero.WriteFile(fs, "/repo/hop.json", []byte("{}"), 0644)
			},
			startPath: "/repo/hops/main",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "bare repo WITHOUT hops/ — not a worktree-shaped repo, NOT detected",
			setup: func(fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "non-bare config with hops/ — NOT detected (regular repo, not bare-worktree-shaped)",
			setup: func(fs afero.Fs) {
				// Non-bare: bare = false
				content := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n"
				_ = afero.WriteFile(fs, "/repo/config", []byte(content), 0644)
				_ = fs.MkdirAll("/repo/hops/main", 0755)
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "empty filesystem — NOT detected",
			setup: func(fs afero.Fs) {
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "bare repo with EMPTY hops/ — NOT detected (no actual worktrees)",
			setup: func(fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				_ = fs.MkdirAll("/repo/hops", 0755)
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			tc.setup(fs)
			gotRoot, gotOK := detectUnregisteredBareWorktreeRepo(fs, tc.startPath)
			if gotOK != tc.wantOK || gotRoot != tc.wantRoot {
				t.Fatalf("detectUnregisteredBareWorktreeRepo(%q) = (%q, %v), want (%q, %v)",
					tc.startPath, gotRoot, gotOK, tc.wantRoot, tc.wantOK)
			}
		})
	}
}
