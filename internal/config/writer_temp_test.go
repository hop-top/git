package config_test

import (
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
)

// crashFs is a filesystem on which a write dies between writing its temp
// file and renaming it: the rename fails and the cleanup never runs.
type crashFs struct{ afero.Fs }

func (crashFs) Rename(string, string) error { return errors.New("crashed") }
func (crashFs) Remove(string) error         { return nil }

// A hop.json write that dies before its rename leaves a temp file,
// named as IsTempName expects, next to hop.json.
func TestWriteHubConfig_CrashLeavesTempNamedAsIsTempName(t *testing.T) {
	mem := afero.NewMemMapFs()
	require.Error(t, config.NewWriter(crashFs{mem}).WriteHubConfig("/hub", &config.HubConfig{}))

	entries, err := afero.ReadDir(mem, "/hub")
	require.NoError(t, err)
	require.Len(t, entries, 1, "the crashed write's temp file")
	assert.True(t, config.IsTempName(entries[0].Name()), "%s", entries[0].Name())
}

func TestIsTempName(t *testing.T) {
	for name, want := range map[string]bool{
		"hop-config-123456789.tmp": true,
		"hop-config-1.tmp":         true,
		"hop-config-.tmp":          false,
		"hop-config-12a.tmp":       false,
		"hop-config-123.tmp.bak":   false,
		"xhop-config-123.tmp":      false,
		"hop.json":                 false,
		"hop.json.lock":            false,
		"state.json.123.tmp":       false,
	} {
		assert.Equal(t, want, config.IsTempName(name), name)
	}
}
