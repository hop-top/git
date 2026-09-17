package cli

import (
	"strings"
	"testing"
)

// TestSetVersion_NoDoubleVPrefix guards against the "vv1.2.3" regression:
// kit's version template renders "v<Config.Version>", so SetVersion must
// strip a leading "v" from a `git describe`-shaped input (which already
// starts with "v") before formatting RootCmd.Version.
func TestSetVersion_NoDoubleVPrefix(t *testing.T) {
	SetVersion("v0.1.0-alpha.1-154-g29a3ae5", "29a3ae5", "2026-09-17T16:08:50Z")

	if RootCmd == nil {
		t.Fatal("RootCmd is nil; init() did not run")
	}
	if strings.HasPrefix(RootCmd.Version, "v ") || strings.Contains(RootCmd.Version, "vv") {
		t.Fatalf("RootCmd.Version has a doubled v prefix: %q", RootCmd.Version)
	}
	if !strings.HasPrefix(RootCmd.Version, "0.1.0-alpha.1-154-g29a3ae5") {
		t.Fatalf("RootCmd.Version = %q, want it to start with the trimmed version", RootCmd.Version)
	}

	rendered := RootCmd.VersionTemplate()
	_ = rendered // template itself is kit's; asserting the trimmed input is the load-bearing check here
}
