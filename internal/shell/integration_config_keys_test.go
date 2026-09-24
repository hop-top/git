package shell_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/shell"
)

// globalHopKeys lists the hop.* keys in the throwaway global git config.
func globalHopKeys(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "config", "--global", "--name-only", "--get-regexp", `^hop\.`).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil // no hop.* keys at all
		}
		t.Fatalf("git config --global --get-regexp: %v", err)
	}
	return strings.Fields(string(out))
}

// assertOnlyShellIntegrationKeys fails for any hop.* key outside
// hop.shellIntegration.* in the global config.
func assertOnlyShellIntegrationKeys(t *testing.T, after string) {
	t.Helper()
	keys := globalHopKeys(t)
	if len(keys) == 0 {
		t.Fatalf("after %s: no hop.* keys written, want hop.shellintegration.*", after)
	}
	for _, k := range keys {
		// git lowercases section and variable names but not subsections;
		// compare case-insensitively rather than depend on either.
		if !strings.HasPrefix(strings.ToLower(k), "hop.shellintegration.") {
			t.Errorf("after %s: global config holds %s; shell integration may only write hop.shellIntegration.*", after, k)
		}
	}
}

// Shell integration records its own state and nothing else. Writing every
// hop.* default alongside it pinned those defaults in --global, where they
// shadow later default changes and read as the user's choice.
func TestShellIntegration_WritesOnlyItsOwnKeys(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(tmp, "gitconfig"))
	t.Setenv("SHELL", "/bin/bash")
	fs := afero.NewOsFs()

	if _, err := shell.InstallIntegration(fs); err != nil {
		t.Fatalf("InstallIntegration: %v", err)
	}
	assertOnlyShellIntegrationKeys(t, "install")

	if err := shell.UninstallIntegration(fs); err != nil {
		t.Fatalf("UninstallIntegration: %v", err)
	}
	assertOnlyShellIntegrationKeys(t, "uninstall")

	if err := shell.SetIntegrationStatus("disabled"); err != nil {
		t.Fatalf("SetIntegrationStatus: %v", err)
	}
	assertOnlyShellIntegrationKeys(t, "status change")

	out, err := exec.Command("git", "config", "--global", "--get", "hop.shellIntegration.status").Output()
	if err != nil || strings.TrimSpace(string(out)) != "disabled" {
		t.Errorf("hop.shellIntegration.status = %q (%v), want disabled", out, err)
	}
}
