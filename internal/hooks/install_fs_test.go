package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// A symlink-mode install creates the link on the filesystem it was given.
func TestInstallHook_SymlinkThroughInjectedFs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "src"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}
	fs := afero.NewBasePathFs(afero.NewOsFs(), root)

	if err := installHook(fs, ModeSymlink, "/src", "/dst", nil); err != nil {
		t.Fatalf("installHook: %v", err)
	}
	target, err := os.Readlink(filepath.Join(root, "dst"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != filepath.Join(root, "src") {
		t.Fatalf("symlink target = %q, want %q", target, filepath.Join(root, "src"))
	}
}

// A filesystem that refuses a removal is not bypassed: the file stays.
func TestRemovePathIfPresent_NoBypassOfInjectedFs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fs := afero.NewReadOnlyFs(afero.NewOsFs())

	if err := removePathIfPresent(fs, path); err == nil {
		t.Error("removePathIfPresent on a read-only fs: want error, got nil")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file removed past a read-only fs: %v", err)
	}
}
