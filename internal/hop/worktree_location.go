package hop

import (
	"path/filepath"
	"strings"
)

// WorktreeLocationContext holds variables for template expansion
type WorktreeLocationContext struct {
	HubPath  string
	Branch   string
	Org      string
	Repo     string
	DataHome string
}

// ExpandWorktreeLocation expands a worktree location pattern into an absolute path
//
// Rules:
// - Empty string "" -> centralized: {dataHome}/{org}/{repo}/hops/{branch}
// - Starts with "/" after expansion -> absolute path from OS root
// - Otherwise -> relative to hubPath
//
// Template variables:
// - {hubPath}  - absolute path to hub root
// - {branch}   - branch name (slashes preserved)
// - {org}      - organization/owner
// - {repo}     - repository name
// - {dataHome} - $GIT_HOP_DATA_HOME
func ExpandWorktreeLocation(pattern string, ctx WorktreeLocationContext) string {
	// Empty string = centralized
	if pattern == "" {
		pattern = "{dataHome}/{org}/{repo}/hops/{branch}"
	}

	// Expand template variables
	expanded := pattern
	expanded = strings.ReplaceAll(expanded, "{hubPath}", ctx.HubPath)
	expanded = strings.ReplaceAll(expanded, "{branch}", ctx.Branch)
	expanded = strings.ReplaceAll(expanded, "{org}", ctx.Org)
	expanded = strings.ReplaceAll(expanded, "{repo}", ctx.Repo)
	expanded = strings.ReplaceAll(expanded, "{dataHome}", ctx.DataHome)

	// If starts with / after expansion, it's absolute
	if strings.HasPrefix(expanded, "/") {
		return expanded
	}

	// Otherwise, relative to hubPath
	return filepath.Join(ctx.HubPath, expanded)
}
