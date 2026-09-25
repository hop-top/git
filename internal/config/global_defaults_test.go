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

// GetDefaults is what Load gives with no hop.* keys; Load reads the git-config
// defaults table. Setting every key in that table explicitly to its default
// value must load exactly GetDefaults: the explicit-value parse path and the
// fallback path agree for every key GlobalConfig models.
func TestGetDefaults_AgreesWithGitConfigDefaultsForEveryKey(t *testing.T) {
	store := map[string]string{}
	for k, v := range defaults {
		store[k] = v
	}
	loader := NewGlobalLoaderWithGitConfig(memGitConfig(store))
	if got, want := readFromGitConfig(loader.gc), loader.GetDefaults(); !reflect.DeepEqual(got, want) {
		t.Errorf("explicit defaults load as %+v\nGetDefaults = %+v", got, want)
	}
}

// With nothing in git config, Load must return exactly GetDefaults.
func TestGetDefaults_EqualsLoadOnEmptyGitConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	loader := NewGlobalLoaderWithGitConfig(memGitConfig(map[string]string{}))
	loaded := loader.Load()
	if defs := loader.GetDefaults(); !reflect.DeepEqual(loaded, defs) {
		t.Errorf("Load on empty git config = %+v\nGetDefaults = %+v", loaded, defs)
	}
}
