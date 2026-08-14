package services_test

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// goPM returns a PackageManager for Go without requiring go on PATH.
// InstallCmd is "true" so an accidental install attempt would still succeed
// and therefore not mask a missing skip-rule with an unrelated error.
func goPM() services.PackageManager {
	return services.PackageManager{
		Name:        "go",
		DetectFiles: []string{"go.mod"},
		LockFiles:   []string{"go.sum"},
		DepsDir:     "vendor",
		InstallCmd:  []string{"true"},
	}
}

// writeGoWorktree lays out a minimal Go worktree on fs. When gitignore is
// non-empty a .gitignore with that content is written.
func writeGoWorktree(t *testing.T, fs afero.Fs, path, gitignore string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(path, 0755))
	require.NoError(t, afero.WriteFile(fs, path+"/go.mod", []byte("module example.com/x\n"), 0644))
	require.NoError(t, afero.WriteFile(fs, path+"/go.sum", []byte("example.com/y v1.0.0 h1:abc=\n"), 0644))
	if gitignore != "" {
		require.NoError(t, afero.WriteFile(fs, path+"/.gitignore", []byte(gitignore), 0644))
	}
}

// TestAudit_NoVendorIssueWhenGitignored is the regression test for doctor
// reporting "missing vendor" in Go repos that gitignore vendor/. The
// worktree-create path already skips vendor via ShouldRunModVendor; Audit
// must apply the same rule instead of flagging an absent vendor/ as
// IssueMissingDeps (which --fix would then materialise).
func TestAudit_NoVendorIssueWhenGitignored(t *testing.T) {
	for _, pattern := range []string{"vendor\n", "vendor/\n", "# deps\nnode_modules/\nvendor/\n"} {
		fs := afero.NewMemMapFs()
		worktree := "/repo/hops/feature"
		writeGoWorktree(t, fs, worktree, pattern)

		registry := &services.DepsRegistry{Entries: map[string]services.DepsEntry{}}
		m := services.NewDepsManagerFromParts(fs, "/repo", registry, []services.PackageManager{goPM()}, nil)

		issues, err := m.Audit(map[string]string{"feature": worktree})
		require.NoError(t, err)
		assert.Empty(t, issues,
			"gitignored vendor/ must not be audited as an issue (gitignore=%q)", pattern)
	}
}

// TestAudit_NoVendorIssueWhenVendorAbsent covers the broader rule inherited
// from ShouldRunModVendor: vendor mode is active only when vendor/ already
// exists in the source tree. A Go repo with no vendor/ and no .gitignore
// must not be told it is missing deps, because --fix would create vendor/.
func TestAudit_NoVendorIssueWhenVendorAbsent(t *testing.T) {
	fs := afero.NewMemMapFs()
	worktree := "/repo/hops/feature"
	writeGoWorktree(t, fs, worktree, "")

	registry := &services.DepsRegistry{Entries: map[string]services.DepsEntry{}}
	m := services.NewDepsManagerFromParts(fs, "/repo", registry, []services.PackageManager{goPM()}, nil)

	issues, err := m.Audit(map[string]string{"feature": worktree})
	require.NoError(t, err)
	assert.Empty(t, issues, "absent vendor/ must not be reported as missing deps")
}

// TestAudit_VendorLocalFolderStillAuditedWhenTracked is the negative case:
// a Go repo that genuinely vendors (vendor/ present, not gitignored) must
// still be inspected so real problems remain visible and fixable.
func TestAudit_VendorLocalFolderStillAuditedWhenTracked(t *testing.T) {
	fs := afero.NewMemMapFs()
	worktree := "/repo/hops/feature"
	writeGoWorktree(t, fs, worktree, "node_modules/\n")
	require.NoError(t, fs.MkdirAll(worktree+"/vendor", 0755))
	require.NoError(t, afero.WriteFile(fs, worktree+"/vendor/modules.txt", []byte("# x\n"), 0644))

	registry := &services.DepsRegistry{Entries: map[string]services.DepsEntry{}}
	m := services.NewDepsManagerFromParts(fs, "/repo", registry, []services.PackageManager{goPM()}, nil)

	issues, err := m.Audit(map[string]string{"feature": worktree})
	require.NoError(t, err)
	require.Len(t, issues, 1, "tracked vendor/ must still be audited")
	assert.Equal(t, services.IssueLocalFolder, issues[0].Type)
	assert.Equal(t, "vendor", issues[0].PM.DepsDir)
}

// TestAudit_NonGoPMUnaffectedByVendorRule guards against over-correcting:
// the vendor rule is Go-specific and must not suppress issues for other
// package managers that also install into vendor/ (composer).
func TestAudit_NonGoPMUnaffectedByVendorRule(t *testing.T) {
	fs := afero.NewMemMapFs()
	worktree := "/repo/hops/feature"
	require.NoError(t, fs.MkdirAll(worktree, 0755))
	require.NoError(t, afero.WriteFile(fs, worktree+"/composer.json", []byte("{}\n"), 0644))
	require.NoError(t, afero.WriteFile(fs, worktree+"/composer.lock", []byte("{}\n"), 0644))
	require.NoError(t, afero.WriteFile(fs, worktree+"/.gitignore", []byte("vendor/\n"), 0644))

	composer := services.PackageManager{
		Name:        "composer",
		DetectFiles: []string{"composer.json"},
		LockFiles:   []string{"composer.lock"},
		DepsDir:     "vendor",
		InstallCmd:  []string{"true"},
	}
	registry := &services.DepsRegistry{Entries: map[string]services.DepsEntry{}}
	m := services.NewDepsManagerFromParts(fs, "/repo", registry, []services.PackageManager{composer}, nil)

	issues, err := m.Audit(map[string]string{"feature": worktree})
	require.NoError(t, err)
	require.Len(t, issues, 1, "composer vendor/ is a regenerable cache and must still be audited")
	assert.Equal(t, services.IssueMissingDeps, issues[0].Type)
}

// TestFix_SkipsGitignoredVendor asserts --fix never materialises vendor/ for
// a gitignored Go worktree, even if a stale IssueMissingDeps is handed to it
// (e.g. from an older audit or a caller that assembled issues by hand).
// Creating vendor/ is the user-visible harm the bug caused.
func TestFix_SkipsGitignoredVendor(t *testing.T) {
	fs := afero.NewMemMapFs()
	worktree := "/repo/hops/feature"
	writeGoWorktree(t, fs, worktree, "vendor/\n")

	registry := &services.DepsRegistry{Entries: map[string]services.DepsEntry{}}
	m := services.NewDepsManagerFromParts(fs, "/repo", registry, []services.PackageManager{goPM()}, nil)

	issue := services.Issue{
		Type:         services.IssueMissingDeps,
		WorktreePath: worktree,
		Branch:       "feature",
		PM:           goPM(),
		ExpectedHash: "abc123",
		DepsKey:      "vendor.abc123",
	}
	require.NoError(t, m.Fix([]services.Issue{issue}, false))

	exists, err := afero.DirExists(fs, worktree+"/vendor")
	require.NoError(t, err)
	assert.False(t, exists, "--fix must not create vendor/ in a gitignored Go worktree")
}

// TestIssueSeverity pins the severity classification doctor relies on to
// decide whether findings make the installation unhealthy. A stale symlink
// means the worktree simply has not re-run its installer since the lockfile
// changed — the deps are present and working — so it is a warning, not an
// error. Everything else is a genuine break needing repair.
func TestIssueSeverity(t *testing.T) {
	tests := []struct {
		issueType services.IssueType
		expected  services.Severity
	}{
		{services.IssueStaleSymlink, services.SeverityWarning},
		{services.IssueBrokenSymlink, services.SeverityError},
		{services.IssueMissingDeps, services.SeverityError},
		{services.IssueLocalFolder, services.SeverityError},
	}
	for _, tt := range tests {
		t.Run(string(tt.issueType), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.issueType.Severity())
		})
	}
}
