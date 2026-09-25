package services

// LayEntryLinksForTest lays depsDir out entry by entry for install.
func (m *DepsManager) LayEntryLinksForTest(install, depsDir string) error {
	return m.layEntryLinks(install, depsDir)
}
