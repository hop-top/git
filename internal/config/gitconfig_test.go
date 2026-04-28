package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTempGitRepo creates an isolated git repo and returns a RunCmd func
// scoped to that repo. Caller should defer cleanup.
func setupTempGitRepo(t *testing.T) (func(args ...string) (string, error), func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "hop-gitconfig-test-*")
	if err != nil {
		t.Fatal(err)
	}

	// Init a repo so --local works
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Return a RunCmd that always runs in the temp dir and avoids
	// touching the user's real global config.
	runCmd := func(args ...string) (string, error) {
		// Rewrite --global to --local so tests stay isolated
		rewritten := make([]string, len(args))
		copy(rewritten, args)
		for i, a := range rewritten {
			if a == "--global" {
				rewritten[i] = "--local"
			}
		}
		cmd := exec.Command("git", rewritten...)
		cmd.Dir = dir
		// Override HOME so git never touches real global config
		cmd.Env = append(os.Environ(),
			"HOME="+dir,
			"GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".gitconfig"),
		)
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		return trimOutput(out), nil
	}

	cleanup := func() { os.RemoveAll(dir) }
	return runCmd, cleanup
}

func trimOutput(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

func TestGetSetBool(t *testing.T) {
	runCmd, cleanup := setupTempGitRepo(t)
	defer cleanup()

	gc := &GitConfig{RunCmd: runCmd}

	// Missing key → error
	_, err := gc.GetBool(KeyBareRepo)
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}

	// Set true, get true
	if err := gc.Set(KeyBareRepo, "true"); err != nil {
		t.Fatal(err)
	}
	v, err := gc.GetBool(KeyBareRepo)
	if err != nil {
		t.Fatal(err)
	}
	if !v {
		t.Fatal("expected true")
	}

	// Set false, get false
	if err := gc.Set(KeyBareRepo, "false"); err != nil {
		t.Fatal(err)
	}
	v, err = gc.GetBool(KeyBareRepo)
	if err != nil {
		t.Fatal(err)
	}
	if v {
		t.Fatal("expected false")
	}
}

func TestGetSetString(t *testing.T) {
	runCmd, cleanup := setupTempGitRepo(t)
	defer cleanup()

	gc := &GitConfig{RunCmd: runCmd}

	// Missing key
	_, err := gc.GetString(KeyGitDomain)
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}

	// Set and get
	if err := gc.Set(KeyGitDomain, "gitlab.com"); err != nil {
		t.Fatal(err)
	}
	v, err := gc.GetString(KeyGitDomain)
	if err != nil {
		t.Fatal(err)
	}
	if v != "gitlab.com" {
		t.Fatalf("expected gitlab.com, got %q", v)
	}
}

func TestGetSetInt(t *testing.T) {
	runCmd, cleanup := setupTempGitRepo(t)
	defer cleanup()

	gc := &GitConfig{RunCmd: runCmd}

	// Missing key
	_, err := gc.GetInt(KeyBackupMaxBackups)
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}

	// Set and get
	if err := gc.Set(KeyBackupMaxBackups, "5"); err != nil {
		t.Fatal(err)
	}
	v, err := gc.GetInt(KeyBackupMaxBackups)
	if err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("expected 5, got %d", v)
	}
}

func TestSetLocal(t *testing.T) {
	runCmd, cleanup := setupTempGitRepo(t)
	defer cleanup()

	gc := &GitConfig{RunCmd: runCmd}

	if err := gc.SetLocal(KeyAutoEnvStart, "true"); err != nil {
		t.Fatal(err)
	}
	v, err := gc.GetBool(KeyAutoEnvStart)
	if err != nil {
		t.Fatal(err)
	}
	if !v {
		t.Fatal("expected true from local config")
	}
}

func TestOrDefaultFallbacks(t *testing.T) {
	runCmd, cleanup := setupTempGitRepo(t)
	defer cleanup()

	gc := &GitConfig{RunCmd: runCmd}

	// Bool default
	if v := gc.GetBoolOrDefault(KeyAutoEnvStart); !v {
		t.Fatal("expected default true for autoEnvStart")
	}

	// String default
	if v := gc.GetStringOrDefault(KeyGitDomain); v != "github.com" {
		t.Fatalf("expected github.com, got %q", v)
	}

	// Int default
	if v := gc.GetIntOrDefault(KeyBackupMaxBackups); v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}

	// Unknown key → zero values
	if v := gc.GetBoolOrDefault("hop.nonexistent"); v {
		t.Fatal("expected false for unknown key")
	}
	if v := gc.GetStringOrDefault("hop.nonexistent"); v != "" {
		t.Fatalf("expected empty, got %q", v)
	}
	if v := gc.GetIntOrDefault("hop.nonexistent"); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

func TestParseBoolVariants(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"true", true}, {"True", true}, {"TRUE", true},
		{"yes", true}, {"on", true}, {"1", true},
		{"false", false}, {"False", false}, {"FALSE", false},
		{"no", false}, {"off", false}, {"0", false},
	}
	for _, tc := range cases {
		got, err := parseBool(tc.in)
		if err != nil {
			t.Fatalf("parseBool(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseBool(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	// Invalid
	_, err := parseBool("maybe")
	if err == nil {
		t.Fatal("expected error for invalid bool")
	}
}

func TestNewGitConfig(t *testing.T) {
	gc := NewGitConfig()
	if gc.RunCmd == nil {
		t.Fatal("RunCmd should not be nil")
	}
}

func TestGetIntInvalidValue(t *testing.T) {
	gc := &GitConfig{
		RunCmd: func(args ...string) (string, error) {
			return "notanumber", nil
		},
	}
	_, err := gc.GetInt(KeyBackupMaxBackups)
	if err == nil {
		t.Fatal("expected error for non-integer value")
	}
}

func TestGetBoolInvalidValue(t *testing.T) {
	gc := &GitConfig{
		RunCmd: func(args ...string) (string, error) {
			return "maybe", nil
		},
	}
	_, err := gc.GetBool(KeyBareRepo)
	if err == nil {
		t.Fatal("expected error for invalid bool value")
	}
}

func TestSetError(t *testing.T) {
	gc := &GitConfig{
		RunCmd: func(args ...string) (string, error) {
			return "", fmt.Errorf("git failed")
		},
	}
	if err := gc.Set(KeyBareRepo, "true"); err == nil {
		t.Fatal("expected error")
	}
	if err := gc.SetLocal(KeyBareRepo, "true"); err == nil {
		t.Fatal("expected error")
	}
}
