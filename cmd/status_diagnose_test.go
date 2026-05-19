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
	// Helpers fail the test via t.Fatalf on any setup error, so a
	// silent setup failure can't masquerade as a passing "not detected"
	// case (relevant because afero.MemMapFs creates parent dirs on
	// WriteFile, but explicit checks keep the test honest if that
	// behavior ever changes).
	mkdir := func(t *testing.T, fs afero.Fs, p string) {
		t.Helper()
		if err := fs.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	writeFile := func(t *testing.T, fs afero.Fs, p string, body string) {
		t.Helper()
		if err := afero.WriteFile(fs, p, []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	cases := []struct {
		name      string
		setup     func(t *testing.T, fs afero.Fs)
		startPath string
		wantRoot  string
		wantOK    bool
	}{
		{
			name: "bare repo with hops/ and no hop.json — detected from root",
			setup: func(t *testing.T, fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				mkdir(t, fs, "/repo/hops/main")
			},
			startPath: "/repo",
			wantRoot:  "/repo",
			wantOK:    true,
		},
		{
			name: "bare repo with hops/ and no hop.json — detected from inside hops/main",
			setup: func(t *testing.T, fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				mkdir(t, fs, "/repo/hops/main")
			},
			startPath: "/repo/hops/main",
			wantRoot:  "/repo",
			wantOK:    true,
		},
		{
			name: "bare repo with hops/ and no hop.json — detected from nested branch path",
			setup: func(t *testing.T, fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				mkdir(t, fs, "/repo/hops/feat/x")
			},
			startPath: "/repo/hops/feat/x",
			wantRoot:  "/repo",
			wantOK:    true,
		},
		{
			name: "bare repo with hops/ AND hop.json — already registered, NOT detected",
			setup: func(t *testing.T, fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				mkdir(t, fs, "/repo/hops/main")
				writeFile(t, fs, "/repo/hop.json", "{}")
			},
			startPath: "/repo/hops/main",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "bare repo WITHOUT hops/ — not a worktree-shaped repo, NOT detected",
			setup: func(t *testing.T, fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "non-bare config with hops/ — NOT detected (regular repo, not bare-worktree-shaped)",
			setup: func(t *testing.T, fs afero.Fs) {
				writeFile(t, fs, "/repo/config",
					"[core]\n\trepositoryformatversion = 0\n\tbare = false\n")
				mkdir(t, fs, "/repo/hops/main")
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "empty filesystem — NOT detected",
			setup: func(t *testing.T, fs afero.Fs) {
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
		{
			name: "bare repo with EMPTY hops/ — NOT detected (no actual worktrees)",
			setup: func(t *testing.T, fs afero.Fs) {
				writeBareConfig(t, fs, "/repo")
				mkdir(t, fs, "/repo/hops")
			},
			startPath: "/repo",
			wantRoot:  "",
			wantOK:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			tc.setup(t, fs)
			gotRoot, gotOK := detectUnregisteredBareWorktreeRepo(fs, tc.startPath)
			if gotOK != tc.wantOK || gotRoot != tc.wantRoot {
				t.Fatalf("detectUnregisteredBareWorktreeRepo(%q) = (%q, %v), want (%q, %v)",
					tc.startPath, gotRoot, gotOK, tc.wantRoot, tc.wantOK)
			}
		})
	}
}
