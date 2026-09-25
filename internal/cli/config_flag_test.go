package cli

import (
	"testing"
)

// The root's --config is the one kit registers, not a local redefinition:
// repeatable, with kit's -c shorthand. git-hop ignores it (settings live in
// git config hop.*), so it keeps kit's default of staying out of --help;
// it stays registered so scripts that pass it do not hit a usage error.
func TestConfigFlagIsKitsAndHidden(t *testing.T) {
	f := RootCmd.PersistentFlags().Lookup("config")
	if f == nil {
		t.Fatal("root registers no --config")
	}
	if f.Shorthand != "c" {
		t.Errorf("--config shorthand = %q, want c", f.Shorthand)
	}
	if got := f.Value.Type(); got != "stringArray" {
		t.Errorf("--config type = %s, want kit's stringArray", got)
	}
	if !f.Hidden {
		t.Error("--config is listed in --help; it is ignored and must stay hidden")
	}
}
