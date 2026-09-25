package config_test

import (
	"reflect"
	"strings"
	"testing"

	"hop.top/git/internal/config"
)

// GlobalLoader must not offer a way to persist a whole GlobalConfig. Such a
// write sets every hop.* scalar, so each compiled default the caller never
// touched lands in --global, where it shadows later default changes and
// reads as the user's own choice. Writers persist only the keys they own
// (see WriteShellIntegration).
func TestGlobalLoader_HasNoWholeConfigWriter(t *testing.T) {
	cfgType := reflect.TypeOf(&config.GlobalConfig{})
	loaderType := reflect.TypeOf(&config.GlobalLoader{})
	for i := 0; i < loaderType.NumMethod(); i++ {
		m := loaderType.Method(i)
		for j := 1; j < m.Type.NumIn(); j++ { // In(0) is the receiver
			if m.Type.In(j) == cfgType {
				t.Errorf("GlobalLoader.%s takes a *GlobalConfig; a whole-config write freezes defaults into --global", m.Name)
			}
		}
	}
}

// WriteShellIntegration leaves every other hop.* key alone: neither live
// settings nor stale or retired keys an older release left behind.
func TestWriteShellIntegration_TouchesOnlyItsOwnKeys(t *testing.T) {
	store := map[string]string{
		"hop.bareRepo":            "not-a-bool",
		"hop.showAllManagedRepos": "true",
		"hop.gitDomain":           "gitlab.com",
	}
	before := make(map[string]string, len(store))
	for k, v := range store {
		before[k] = v
	}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))

	s := loader.GetDefaults().ShellIntegration
	s.Status = "approved"
	if err := loader.WriteShellIntegration(s); err != nil {
		t.Fatalf("WriteShellIntegration() error = %v", err)
	}
	if store[config.KeyShellIntegrationStatus] != "approved" {
		t.Errorf("%s = %q, want approved", config.KeyShellIntegrationStatus, store[config.KeyShellIntegrationStatus])
	}
	for k, v := range store {
		if strings.HasPrefix(k, "hop.shellIntegration.") {
			continue
		}
		if before[k] != v {
			t.Errorf("WriteShellIntegration() set %s = %q (was %q)", k, v, before[k])
		}
	}
}
