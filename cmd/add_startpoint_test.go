package cmd

import "testing"

func TestResolveAddStartPoint_FlagBeatsEnvBeatsConfig(t *testing.T) {
	tests := []struct {
		name              string
		flag, env, config string
		want              string
	}{
		{"flag wins over env+config", "fromflag", "fromenv", "fromcfg", "fromflag"},
		{"flag wins over env", "fromflag", "fromenv", "", "fromflag"},
		{"flag wins over config", "fromflag", "", "fromcfg", "fromflag"},
		{"env wins over config when no flag", "", "fromenv", "fromcfg", "fromenv"},
		{"config used when no flag/env", "", "", "fromcfg", "fromcfg"},
		{"all empty -> empty (manager applies built-in default)", "", "", "", ""},
		{"explicit 'initial' through flag", "initial", "", "default-branch", "initial"},
		{"explicit 'default-branch' through config", "", "", "default-branch", "default-branch"},
		{"explicit SHA through env", "", "abc1234", "", "abc1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAddStartPoint(tt.flag, tt.env, tt.config)
			if got != tt.want {
				t.Errorf("resolveAddStartPoint(%q,%q,%q) = %q, want %q",
					tt.flag, tt.env, tt.config, got, tt.want)
			}
		})
	}
}
