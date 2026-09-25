package cli

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// GIT_HOP_VERBOSE reads like git's GIT_TRACE-style switches: a boolean
// word, case-insensitive, or a -V count. Anything else is not a value.
func TestParseVerboseEnv(t *testing.T) {
	cases := []struct {
		raw  string
		want int
		ok   bool
	}{
		{"", 0, true},
		{"0", 0, true},
		{"1", 1, true},
		{"2", 2, true},
		{"3", 3, true},
		{"true", 1, true},
		{"TRUE", 1, true},
		{"yes", 1, true},
		{"Yes", 1, true},
		{"on", 1, true},
		{"ON", 1, true},
		{"false", 0, true},
		{"False", 0, true},
		{"no", 0, true},
		{"NO", 0, true},
		{"off", 0, true},
		{"Off", 0, true},
		{"loud", 0, false},
		{"-1", 0, false},
		{"1.5", 0, false},
		{" 1", 0, false},
		{"y", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseVerboseEnv(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseVerboseEnv(%q) = %d, %v; want %d, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

// newVerboseViper wires a viper the way the root does: kit's --verbose
// count flag bound to the "verbose" key, the command line parsed, then
// GIT_HOP_VERBOSE applied as the pre-run applies it.
func newVerboseViper(t *testing.T, args ...string) (*viper.Viper, string) {
	t.Helper()
	fs := pflag.NewFlagSet("git-hop", pflag.ContinueOnError)
	fs.CountP("verbose", "V", "")
	v := viper.New()
	if err := v.BindPFlag("verbose", fs.Lookup("verbose")); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return v, applyVerboseEnv(v)
}

func TestVerboseEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string // "" counts as unset
		args []string
		want int
	}{
		{"unset", "", nil, 0},
		{"1 is -V", "1", nil, 1},
		{"2 is -VV", "2", nil, 2},
		{"0 is off", "0", nil, 0},
		{"true is -V", "true", nil, 1},
		{"On is -V", "On", nil, 1},
		{"off is off", "off", nil, 0},
		{"-V wins over 2", "2", []string{"-V"}, 1},
		{"--verbose=0 wins over 1", "1", []string{"--verbose=0"}, 0},
		{"--verbose=0 wins over yes", "yes", []string{"--verbose=0"}, 0},
		{"-VV wins over 0", "0", []string{"-VV"}, 2},
		{"flag alone", "", []string{"-VVV"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(verboseEnv, tc.env)
			v, warning := newVerboseViper(t, tc.args...)
			if got := v.GetInt("verbose"); got != tc.want {
				t.Errorf("verbose = %d, want %d", got, tc.want)
			}
			if warning != "" {
				t.Errorf("unexpected warning %q", warning)
			}
		})
	}
}

// A value that is neither a boolean word nor a count is a warning naming
// the variable, and counts as 0.
func TestVerboseEnv_Invalid(t *testing.T) {
	for _, raw := range []string{"loud", "-1", "y"} {
		t.Setenv(verboseEnv, raw)
		v, warning := newVerboseViper(t)
		if got := v.GetInt("verbose"); got != 0 {
			t.Errorf("%s=%q: verbose = %d, want 0", verboseEnv, raw, got)
		}
		if !strings.Contains(warning, verboseEnv) || !strings.Contains(warning, raw) {
			t.Errorf("%s=%q: warning %q does not name the variable and value", verboseEnv, raw, warning)
		}
	}
	// The next run with a valid value drops what an earlier one applied.
	t.Setenv(verboseEnv, "2")
	v, _ := newVerboseViper(t)
	applyVerboseEnv(v)
	t.Setenv(verboseEnv, "loud")
	applyVerboseEnv(v)
	if got := v.GetInt("verbose"); got != 0 {
		t.Errorf("after 2 then loud: verbose = %d, want 0", got)
	}
}

// Only GIT_HOP_VERBOSE is read: the other global settings kit binds to
// the root's viper have no GIT_HOP_* variable.
func TestNoOtherGitHopEnvBinding(t *testing.T) {
	for key, env := range map[string]string{
		"quiet":    "GIT_HOP_QUIET",
		"format":   "GIT_HOP_FORMAT",
		"no-color": "GIT_HOP_NO_COLOR",
		"output":   "GIT_HOP_OUTPUT",
		"cols":     "GIT_HOP_COLS",
		"template": "GIT_HOP_TEMPLATE",
		"offline":  "GIT_HOP_OFFLINE",
	} {
		t.Setenv(env, "1")
		if Root.Viper.IsSet(key) {
			t.Errorf("%s sets %q in the root viper", env, key)
		}
	}
}
