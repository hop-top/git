package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
)

// newInitKeepBackupFlags parses argv against a fresh --keep-backup flag
// declared exactly as init declares it, so Changed() behaves as it does
// for a real invocation.
func newInitKeepBackupFlags(t *testing.T, argv ...string) *cobra.Command {
	t.Helper()
	def := initCmd.Flags().Lookup("keep-backup")
	require.NotNil(t, def, "init must declare --keep-backup")
	c := &cobra.Command{Use: "init"}
	var v bool
	c.Flags().BoolVar(&v, def.Name, def.DefValue == "true", def.Usage)
	require.NoError(t, c.Flags().Parse(argv))
	return c
}

// hop.backup.keepBackup supplies the default; a typed flag, either way,
// wins.
func TestResolveInitKeepBackup(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		cfg  map[string]string
		want bool
	}{
		{name: "neither set: backup removed", want: false},
		{name: "config on", cfg: map[string]string{config.KeyBackupKeepBackup: "true"}, want: true},
		{name: "config off", cfg: map[string]string{config.KeyBackupKeepBackup: "false"}, want: false},
		{name: "git bool spelling", cfg: map[string]string{config.KeyBackupKeepBackup: "yes"}, want: true},
		{name: "flag on, config off", argv: []string{"--keep-backup"}, cfg: map[string]string{config.KeyBackupKeepBackup: "false"}, want: true},
		{name: "explicit flag off beats config on", argv: []string{"--keep-backup=false"}, cfg: map[string]string{config.KeyBackupKeepBackup: "true"}, want: false},
		{name: "unparseable config falls back to default", cfg: map[string]string{config.KeyBackupKeepBackup: "banana"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newInitKeepBackupFlags(t, tt.argv...)
			v, err := c.Flags().GetBool("keep-backup")
			require.NoError(t, err)
			got := resolveInitKeepBackup(v, c.Flags().Changed("keep-backup"), stubGitConfig(tt.cfg))
			assert.Equal(t, tt.want, got)
		})
	}
}
