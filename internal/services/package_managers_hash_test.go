package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
)

func TestPackageManager_HashLockfile(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	lockfilePath := filepath.Join(tmpDir, "test.lock")

	// Write test content
	content := []byte("test lockfile content\nwith multiple lines\n")
	if err := os.WriteFile(lockfilePath, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	// Hash the lockfile
	hash1, err := pm.HashLockfile(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfile() error = %v", err)
	}

	// Verify hash is 6 characters
	if len(hash1) != 6 {
		t.Errorf("HashLockfile() hash length = %d, want 6", len(hash1))
	}

	// Verify hash is consistent
	hash2, err := pm.HashLockfile(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfile() second call error = %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("HashLockfile() inconsistent: first=%s, second=%s", hash1, hash2)
	}

	// Verify different content produces different hash
	newContent := []byte("different content\n")
	if err := os.WriteFile(lockfilePath, newContent, 0644); err != nil {
		t.Fatalf("failed to update test file: %v", err)
	}

	hash3, err := pm.HashLockfile(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfile() third call error = %v", err)
	}

	if hash1 == hash3 {
		t.Errorf("HashLockfile() should produce different hash for different content")
	}
}

func TestPackageManager_HashLockfileLong(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	lockfilePath := filepath.Join(tmpDir, "test.lock")

	// Write test content
	content := []byte("test lockfile content for long hash\n")
	if err := os.WriteFile(lockfilePath, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	// Hash the lockfile with long hash
	longHash1, err := pm.HashLockfileLong(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfileLong() error = %v", err)
	}

	// Verify hash is 12 characters
	if len(longHash1) != 12 {
		t.Errorf("HashLockfileLong() hash length = %d, want 12", len(longHash1))
	}

	// Verify short hash is prefix of long hash
	shortHash, err := pm.HashLockfile(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfile() error = %v", err)
	}

	if len(longHash1) < len(shortHash) || longHash1[:len(shortHash)] != shortHash {
		t.Errorf("HashLockfileLong() should have HashLockfile() as prefix. short=%s, long=%s", shortHash, longHash1)
	}

	// Verify consistency
	longHash2, err := pm.HashLockfileLong(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfileLong() second call error = %v", err)
	}

	if longHash1 != longHash2 {
		t.Errorf("HashLockfileLong() inconsistent: first=%s, second=%s", longHash1, longHash2)
	}
}

func TestPackageManager_HashCollisionScenario(t *testing.T) {
	// This test simulates a hash collision scenario
	// Create two different lockfiles that might have same short hash
	tmpDir := t.TempDir()
	lockfile1 := filepath.Join(tmpDir, "lock1.json")
	lockfile2 := filepath.Join(tmpDir, "lock2.json")

	// Create different content
	content1 := []byte(`{"dependencies": {"pkg1": "1.0.0"}}`)
	content2 := []byte(`{"dependencies": {"pkg2": "2.0.0"}}`)

	if err := os.WriteFile(lockfile1, content1, 0644); err != nil {
		t.Fatalf("failed to create lockfile1: %v", err)
	}
	if err := os.WriteFile(lockfile2, content2, 0644); err != nil {
		t.Fatalf("failed to create lockfile2: %v", err)
	}

	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	// Get short hashes
	hash1, err := pm.HashLockfile(fs, lockfile1)
	if err != nil {
		t.Fatalf("HashLockfile(lockfile1) error = %v", err)
	}

	hash2, err := pm.HashLockfile(fs, lockfile2)
	if err != nil {
		t.Fatalf("HashLockfile(lockfile2) error = %v", err)
	}

	// Different content should produce different short hashes (in most cases)
	// But if they were the same, long hashes should differ
	if hash1 == hash2 {
		// Collision detected in short hash - verify long hashes differ
		longHash1, err := pm.HashLockfileLong(fs, lockfile1)
		if err != nil {
			t.Fatalf("HashLockfileLong(lockfile1) error = %v", err)
		}

		longHash2, err := pm.HashLockfileLong(fs, lockfile2)
		if err != nil {
			t.Fatalf("HashLockfileLong(lockfile2) error = %v", err)
		}

		if longHash1 == longHash2 {
			t.Errorf("Long hashes should differ even with short hash collision. long1=%s, long2=%s", longHash1, longHash2)
		}
	}
}

func TestPackageManager_HashError_FileNotFound(t *testing.T) {
	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	// Try to hash non-existent file
	_, err := pm.HashLockfile(fs, "/nonexistent/path/to/lockfile.json")
	if err == nil {
		t.Error("HashLockfile() should return error for non-existent file")
	}

	_, err = pm.HashLockfileLong(fs, "/nonexistent/path/to/lockfile.json")
	if err == nil {
		t.Error("HashLockfileLong() should return error for non-existent file")
	}
}

func TestPackageManager_HashEmptyFile(t *testing.T) {
	// Test hashing an empty file
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.lock")

	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	hash, err := pm.HashLockfile(fs, emptyFile)
	if err != nil {
		t.Fatalf("HashLockfile() error = %v", err)
	}

	// Empty file should still produce a valid hash
	if len(hash) != 6 {
		t.Errorf("HashLockfile() on empty file hash length = %d, want 6", len(hash))
	}

	longHash, err := pm.HashLockfileLong(fs, emptyFile)
	if err != nil {
		t.Fatalf("HashLockfileLong() error = %v", err)
	}

	if len(longHash) != 12 {
		t.Errorf("HashLockfileLong() on empty file hash length = %d, want 12", len(longHash))
	}
}

func TestPackageManager_HashLargeFile(t *testing.T) {
	// Test hashing a large file
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.lock")

	// Create a file with 1MB of content
	content := make([]byte, 1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	if err := os.WriteFile(largeFile, content, 0644); err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}

	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	hash, err := pm.HashLockfile(fs, largeFile)
	if err != nil {
		t.Fatalf("HashLockfile() error = %v", err)
	}

	if len(hash) != 6 {
		t.Errorf("HashLockfile() hash length = %d, want 6", len(hash))
	}

	longHash, err := pm.HashLockfileLong(fs, largeFile)
	if err != nil {
		t.Fatalf("HashLockfileLong() error = %v", err)
	}

	if len(longHash) != 12 {
		t.Errorf("HashLockfileLong() hash length = %d, want 12", len(longHash))
	}
}

func TestPackageManager_HashDifferentFormats(t *testing.T) {
	// Test hashing different lockfile formats
	tmpDir := t.TempDir()
	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "JSON lockfile",
			content: `{"lockfileVersion": 2, "dependencies": {"pkg": "1.0.0"}}`,
		},
		{
			name:    "YAML lockfile",
			content: "lockfileVersion: 5.4\ndependencies:\n  pkg: 1.0.0\n",
		},
		{
			name:    "Plain text lockfile",
			content: "pkg@1.0.0:\n  version: 1.0.0\n  resolved: https://registry.example.com/pkg-1.0.0.tgz\n",
		},
		{
			name:    "Binary-like content",
			content: string([]byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE}),
		},
	}

	hashes := make(map[string]string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lockfile := filepath.Join(tmpDir, tt.name+".lock")
			if err := os.WriteFile(lockfile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}

			hash, err := pm.HashLockfile(fs, lockfile)
			if err != nil {
				t.Fatalf("HashLockfile() error = %v", err)
			}

			if len(hash) != 6 {
				t.Errorf("HashLockfile() hash length = %d, want 6", len(hash))
			}

			// Store hash to check for uniqueness
			hashes[tt.name] = hash
		})
	}

	// Verify all hashes are different (different content should produce different hashes)
	seenHashes := make(map[string]string)
	for name, hash := range hashes {
		if other, exists := seenHashes[hash]; exists {
			t.Errorf("Hash collision: %s and %s have same hash %s", name, other, hash)
		}
		seenHashes[hash] = name
	}
}

func TestPackageManager_HashStability(t *testing.T) {
	// Verify that hash is stable across multiple calls and doesn't depend on timing
	tmpDir := t.TempDir()
	lockfile := filepath.Join(tmpDir, "stable.lock")
	content := []byte("stable content for testing\n")

	if err := os.WriteFile(lockfile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	fs := afero.NewOsFs()
	pm := services.PackageManager{Name: "test"}

	// Hash multiple times
	var hashes []string
	var longHashes []string

	for i := 0; i < 10; i++ {
		hash, err := pm.HashLockfile(fs, lockfile)
		if err != nil {
			t.Fatalf("HashLockfile() iteration %d error = %v", i, err)
		}
		hashes = append(hashes, hash)

		longHash, err := pm.HashLockfileLong(fs, lockfile)
		if err != nil {
			t.Fatalf("HashLockfileLong() iteration %d error = %v", i, err)
		}
		longHashes = append(longHashes, longHash)
	}

	// Verify all short hashes are identical
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("Hash stability failed: iteration %d hash = %s, want %s", i, hashes[i], hashes[0])
		}
	}

	// Verify all long hashes are identical
	for i := 1; i < len(longHashes); i++ {
		if longHashes[i] != longHashes[0] {
			t.Errorf("Long hash stability failed: iteration %d hash = %s, want %s", i, longHashes[i], longHashes[0])
		}
	}
}

func TestPackageManager_GetDepsKey_WithHashes(t *testing.T) {
	// Test GetDepsKey with actual hashes
	tmpDir := t.TempDir()
	lockfile := filepath.Join(tmpDir, "test.lock")
	content := []byte("test content\n")

	if err := os.WriteFile(lockfile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	fs := afero.NewOsFs()

	tests := []struct {
		name           string
		pm             services.PackageManager
		expectedPrefix string
	}{
		{
			name: "npm with short hash",
			pm: services.PackageManager{
				Name:    "npm",
				DepsDir: "node_modules",
			},
			expectedPrefix: "node_modules.",
		},
		{
			name: "bundler with nested path",
			pm: services.PackageManager{
				Name:    "bundler",
				DepsDir: "vendor/bundle",
			},
			expectedPrefix: "vendor_bundle.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := tt.pm.HashLockfile(fs, lockfile)
			if err != nil {
				t.Fatalf("HashLockfile() error = %v", err)
			}

			depsKey := tt.pm.GetDepsKey(hash)

			// Verify key format
			if len(depsKey) < len(tt.expectedPrefix)+6 {
				t.Errorf("GetDepsKey() = %s, too short", depsKey)
			}

			// Verify prefix
			if len(depsKey) < len(tt.expectedPrefix) || depsKey[:len(tt.expectedPrefix)] != tt.expectedPrefix {
				t.Errorf("GetDepsKey() = %s, want prefix %s", depsKey, tt.expectedPrefix)
			}

			// Verify hash suffix
			expectedKey := tt.expectedPrefix + hash
			if depsKey != expectedKey {
				t.Errorf("GetDepsKey() = %s, want %s", depsKey, expectedKey)
			}
		})
	}
}

func TestPackageManager_HashWithAferoFs(t *testing.T) {
	// HashLockfile now accepts afero.Fs, so it works with in-memory FS
	fs := afero.NewMemMapFs()
	worktreePath := "/test/worktree"

	if err := fs.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Create a lockfile in the in-memory FS
	lockfilePath := filepath.Join(worktreePath, "test.lock")
	if err := afero.WriteFile(fs, lockfilePath, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create lockfile: %v", err)
	}

	pm := services.PackageManager{Name: "test"}

	// HashLockfile now uses afero.Fs, so this should succeed
	hash, err := pm.HashLockfile(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfile() should succeed with in-memory FS: %v", err)
	}

	if len(hash) != 6 {
		t.Errorf("HashLockfile() hash length = %d, want 6", len(hash))
	}

	longHash, err := pm.HashLockfileLong(fs, lockfilePath)
	if err != nil {
		t.Fatalf("HashLockfileLong() should succeed with in-memory FS: %v", err)
	}

	if len(longHash) != 12 {
		t.Errorf("HashLockfileLong() hash length = %d, want 12", len(longHash))
	}
}
