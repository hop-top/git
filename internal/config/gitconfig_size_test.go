package config

import (
	"fmt"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"10m", 10 << 20},
		{"10M", 10 << 20},
		{"512k", 512 << 10},
		{"512K", 512 << 10},
		{"2g", 2 << 30},
		{"2G", 2 << 30},
		{"  10m  ", 10 << 20},
	}
	for _, tc := range tests {
		got, err := ParseSize(tc.in)
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseSize_Rejects(t *testing.T) {
	// A negative ceiling would disable the guard it configures, so it is
	// an error rather than a silently-accepted value.
	for _, in := range []string{"", "abc", "10x", "-1", "-5m", "1.5m"} {
		if got, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) = %d, want an error", in, got)
		}
	}
}

func TestGetSizeOrDefault(t *testing.T) {
	t.Run("reads a configured value", func(t *testing.T) {
		gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
			return "512k", nil
		}}
		if got := gc.GetSizeOrDefault(KeyAddCopyIgnoredMaxSize); got != 512<<10 {
			t.Errorf("got %d, want %d", got, 512<<10)
		}
	})

	t.Run("falls back when the key is missing", func(t *testing.T) {
		gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
			return "", fmt.Errorf("boom")
		}}
		if got := gc.GetSizeOrDefault(KeyAddCopyIgnoredMaxSize); got != 10<<20 {
			t.Errorf("got %d, want the 10m default", got)
		}
	})

	// A typo'd size must not break `git hop add`; it falls back to the
	// compiled default.
	t.Run("falls back when the value is unparseable", func(t *testing.T) {
		gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
			return "ten megabytes", nil
		}}
		if got := gc.GetSizeOrDefault(KeyAddCopyIgnoredMaxSize); got != 10<<20 {
			t.Errorf("got %d, want the 10m default", got)
		}
	})
}

// The opt-out key must default to on, so the feature works without setup,
// and must honour git's full range of boolean spellings.
func TestCopyIgnoredConfigKey_DefaultsOn(t *testing.T) {
	gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
		return "", fmt.Errorf("missing")
	}}
	if !gc.GetBoolOrDefault(KeyAddCopyIgnored) {
		t.Error("hop.add.copyIgnored should default to true")
	}

	for _, off := range []string{"false", "no", "off", "0"} {
		gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
			return off, nil
		}}
		if gc.GetBoolOrDefault(KeyAddCopyIgnored) {
			t.Errorf("value %q should disable the feature", off)
		}
	}
}
