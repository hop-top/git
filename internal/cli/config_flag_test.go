package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func writeJSON(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The root's --config is the one kit registers, not a local redefinition:
// repeatable, with kit's -c shorthand, and listed in --help.
func TestConfigFlagIsKits(t *testing.T) {
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
	if f.Hidden {
		t.Error("--config is hidden from --help")
	}
}

func TestLoadConfigDefaultFile(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "config.json"), `{"a":"default"}`)

	v := viper.New()
	loadConfig(v, nil, nil, dir)

	if got := v.GetString("a"); got != "default" {
		t.Errorf("a = %q, want the default file's value", got)
	}
}

// An explicit file replaces the default one; it is not layered on top.
func TestLoadConfigExplicitFileReplacesDefault(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "config.json"), `{"a":"default","b":"default"}`)
	explicit := writeJSON(t, filepath.Join(t.TempDir(), "explicit.json"), `{"a":"explicit"}`)

	v := viper.New()
	loadConfig(v, []string{explicit}, nil, dir)

	if got := v.GetString("a"); got != "explicit" {
		t.Errorf("a = %q, want the explicit file's value", got)
	}
	if got := v.GetString("b"); got != "" {
		t.Errorf("b = %q, want unset: the default file must not be read", got)
	}
	if got := v.ConfigFileUsed(); got != explicit {
		t.Errorf("config file used = %q, want %q", got, explicit)
	}
}

// Repeated files layer in order, the later one winning.
func TestLoadConfigLaterFileWins(t *testing.T) {
	tmp := t.TempDir()
	first := writeJSON(t, filepath.Join(tmp, "first.json"), `{"a":"first","b":"first"}`)
	second := writeJSON(t, filepath.Join(tmp, "second.json"), `{"a":"second"}`)

	v := viper.New()
	loadConfig(v, []string{first, second}, nil, t.TempDir())

	if got := v.GetString("a"); got != "second" {
		t.Errorf("a = %q, want second", got)
	}
	if got := v.GetString("b"); got != "first" {
		t.Errorf("b = %q, want first", got)
	}
}

// key=value overrides win over every file but lose to the environment.
func TestLoadConfigOverridePrecedence(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "config.json"), `{"a":"file","b":"file","nested":{"k":"file"}}`)
	t.Setenv("GIT_HOP_B", "env")

	v := viper.New()
	loadConfig(v, nil, map[string]any{
		"a":      "override",
		"b":      "override",
		"nested": map[string]any{"k": "override"},
	}, dir)

	if got := v.GetString("a"); got != "override" {
		t.Errorf("a = %q, want override over file", got)
	}
	if got := v.GetString("b"); got != "env" {
		t.Errorf("b = %q, want env over override", got)
	}
	if got := v.GetString("nested.k"); got != "override" {
		t.Errorf("nested.k = %q, want override", got)
	}
}
