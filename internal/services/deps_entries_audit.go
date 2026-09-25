package services

import (
	"fmt"
	"strings"
)

// IssueEntryLinks marks a DepsDir in the per-entry layout (see
// deps_entries.go) missing links into the install for the current
// lockfile: removed (rm -rf node_modules/*), dangling, or pointing
// elsewhere in the store. The next install lays them again.
const IssueEntryLinks IssueType = "entry_links"

// auditEntryLinks classifies depsDir, a real directory linked entry by
// entry into install. Something real where a link belongs means the
// package manager installed into the worktree (npm install replaces every
// link): a local folder, which the next install trashes and relinks.
func (m *DepsManager) auditEntryLinks(issue Issue, depsDir, install string) (Issue, bool) {
	issue.SymlinkTarget = install
	// An install that cannot be read (gone) leaves every link dangling.
	check, err := m.checkEntryLinks(install, depsDir)
	if err != nil {
		issue.Type = IssueBrokenSymlink
		return issue, true
	}
	switch {
	case len(check.Shadowed) > 0:
		issue.Type = IssueLocalFolder
		issue.Size = m.getDirSize(depsDir)
	case m.damagedStoreInstall(issue.PM, install):
		issue.Type = IssueDamagedInstall
	case m.unshareableStoreInstall(issue.PM, install, &issue):
		issue.Type = IssueNeedsLocal
	case install != m.getDepsPath(issue.DepsKey):
		issue.Type = IssueStaleSymlink
	case len(check.Missing) > 0:
		issue.Type = IssueEntryLinks
		issue.Missing = check.Missing
	default:
		return Issue{}, false
	}
	return issue, true
}

// MissingSummary lists the entries an IssueEntryLinks is missing links
// for, the first few by name.
func (i Issue) MissingSummary() string {
	const shown = 3
	names := i.Missing
	if len(names) <= shown {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:shown], ", "), len(names)-shown)
}
