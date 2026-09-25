package cmd

import (
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The preserved-backup line reflects the disk, not the warning count: a
// backup the converter already deleted is never reported as preserved.
func TestReportPreservedBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	kept := "/cache/git-hop/org-repo/2026-01-01_00-00-00"
	require.NoError(t, fs.MkdirAll(kept, 0o755))

	out := captureStdout(t, func() { reportPreservedBackup(os.Stdout, fs, kept) })
	assert.Contains(t, out, "Backup preserved at: "+kept)

	gone := "/cache/git-hop/org-repo/2026-01-02_00-00-00"
	out = captureStdout(t, func() { reportPreservedBackup(os.Stdout, fs, gone) })
	assert.NotContains(t, out, "Backup preserved at")

	out = captureStdout(t, func() { reportPreservedBackup(os.Stdout, fs, "") })
	assert.NotContains(t, out, "Backup preserved at")
}
