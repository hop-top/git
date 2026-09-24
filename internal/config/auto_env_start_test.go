package config

import (
	"strings"
	"testing"
)

func TestResolveAutoEnvStart(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name       string
		override   *bool
		env        string
		configured bool
		want       bool
	}{
		{"config off", nil, "", false, false},
		{"config on", nil, "", true, true},
		{"env on beats config off", nil, "true", false, true},
		{"env off beats config on", nil, "false", true, false},
		{"env takes git spellings", nil, "YES", false, true},
		{"env 0", nil, "0", true, false},
		{"flag on beats env off", &on, "false", false, true},
		{"flag off beats env on", &off, "true", true, false},
		{"flag wins over a bad env value", &off, "maybe", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAutoEnvStart(tt.override, tt.env, tt.configured)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveAutoEnvStart_BadEnvValue(t *testing.T) {
	_, err := ResolveAutoEnvStart(nil, "maybe", true)
	if err == nil {
		t.Fatal("want an error for a non-boolean value")
	}
	want := "bad boolean environment value 'maybe' for 'GIT_HOP_AUTO_ENV_START'"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// Hubs initialized before init stopped writing it carry
// settings.autoEnvStart in hop.json. HubSettings does not model it; such a
// file must still load, and a rewrite must not drop the key.
func TestHubConfig_LegacyAutoEnvStartSettingSurvives(t *testing.T) {
	legacy := strings.Replace(cloneShapedHopJSON,
		`"envPatterns": ["dev", "staging", "qa"]`,
		`"autoEnvStart": true, "envPatterns": ["dev"]`, 1)
	fs := seedHopJSON(t, legacy)

	cfg, err := NewLoader(fs).LoadHubConfig("/hub")
	if err != nil {
		t.Fatalf("legacy hop.json did not load: %v", err)
	}
	if err := NewWriter(fs).WriteHubConfig("/hub", cfg); err != nil {
		t.Fatal(err)
	}
	if got := obj(t, readHopJSON(t, fs), "settings")["autoEnvStart"]; got != true {
		t.Errorf("settings.autoEnvStart = %v after rewrite, want true", got)
	}
}

func TestDefaults_EnvAutoStartOff(t *testing.T) {
	gc := &GitConfig{RunCmd: func(...string) (string, error) { return "", ErrKeyNotFound }}
	if gc.GetBoolOrDefault(KeyEnvAutoStart) {
		t.Error("hop.env.autoStart defaults to true, want false")
	}
}

// hop.autoEnvStart is retired, not renamed-and-aliased: earlier shell
// integration installs pinned it to true in --global without the user
// choosing it, so reading it would turn the start on for them.
func TestRetiredAutoEnvStartIsNotRead(t *testing.T) {
	gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
		if args[len(args)-1] == "hop.autoEnvStart" {
			return "true", nil
		}
		return "", ErrKeyNotFound
	}}
	if readFromGitConfig(gc).Defaults.EnvAutoStart {
		t.Error("EnvAutoStart = true from the retired hop.autoEnvStart, want false")
	}
}
