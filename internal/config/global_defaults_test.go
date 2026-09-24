package config

import (
	"fmt"
	"reflect"
	"testing"
)

func memGitConfig(store map[string]string) *GitConfig {
	return &GitConfig{
		RunCmd: func(args ...string) (string, error) {
			if len(args) == 3 && args[0] == "config" && args[1] == "--get" {
				if v, ok := store[args[2]]; ok {
					return v, nil
				}
				return "", ErrKeyNotFound
			}
			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				store[args[2]] = args[3]
				return "", nil
			}
			return "", fmt.Errorf("unexpected args: %v", args)
		},
	}
}

// GetDefaults is the fallback when Load fails; Load reads the git-config
// defaults table. Every hop.* key GlobalConfig models must agree between the
// two, and keys without a table entry must default to the zero value.
func TestGetDefaults_AgreesWithGitConfigDefaultsForEveryKey(t *testing.T) {
	store := map[string]string{}
	loader := NewGlobalLoaderWithGitConfig(memGitConfig(store))
	if err := loader.writeToGitConfig(loader.GetDefaults()); err != nil {
		t.Fatalf("writeToGitConfig: %v", err)
	}
	if len(store) == 0 {
		t.Fatal("writeToGitConfig wrote no keys")
	}

	for key, got := range store {
		want, ok := defaults[key]
		if !ok {
			if got != "" && got != "false" && got != "0" {
				t.Errorf("%s: GetDefaults gives %q but the git-config defaults table has no entry", key, got)
			}
			continue
		}
		if got != want {
			t.Errorf("%s: GetDefaults gives %q, git-config default is %q", key, got, want)
		}
	}
}

// With nothing in git config, Load must return exactly GetDefaults.
func TestGetDefaults_EqualsLoadOnEmptyGitConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	loader := NewGlobalLoaderWithGitConfig(memGitConfig(map[string]string{}))
	loaded, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if defs := loader.GetDefaults(); !reflect.DeepEqual(loaded, defs) {
		t.Errorf("Load on empty git config = %+v\nGetDefaults = %+v", loaded, defs)
	}
}
