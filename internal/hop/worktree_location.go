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
	// URI is the repository's origin URL. With Org and Repo it names the
	// repository's hopspace ({hopspace}); it supplies the host when
	// hop.dataLayout names {host}.
	URI string
}

// CentralizedWorktreeLocation is the pattern an empty hop.worktreeLocation
// stands for: worktrees inside the repository's data-home hopspace.
const CentralizedWorktreeLocation = "{hopspace}/hops/{branch}"

// ExpandWorktreeLocation expands a worktree location pattern into an absolute path
//
// Rules:
// - Empty string "" -> centralized: {hopspace}/hops/{branch}
// - Starts with "/" after expansion -> absolute path from OS root
// - Otherwise -> relative to hubPath
//
// Template variables:
//   - {hubPath}  - absolute path to hub root
//   - {branch}   - branch name (slashes preserved)
//   - {org}      - organization/owner
//   - {repo}     - repository name
//   - {dataHome} - $GIT_HOP_DATA_HOME
//   - {hopspace} - the repository's data-home hopspace, {dataHome} joined
//     with hop.dataLayout as the hub resolves it (GetHopspacePath)
func ExpandWorktreeLocation(pattern string, ctx WorktreeLocationContext) string {
	if pattern == "" {
		pattern = CentralizedWorktreeLocation
	}

	pairs := []string{
		"{hubPath}", ctx.HubPath,
		"{branch}", ctx.Branch,
		"{org}", ctx.Org,
		"{repo}", ctx.Repo,
		"{dataHome}", ctx.DataHome,
	}
	// Resolving the hopspace reads git config; only pay for it when used.
	if strings.Contains(pattern, "{hopspace}") {
		ref := NewRepoRef(ctx.URI, ctx.Org, ctx.Repo).In(ctx.HubPath)
		pairs = append(pairs, "{hopspace}", GetHopspacePath(ctx.DataHome, ref))
	}
	// One pass, so a value is never itself expanded.
	expanded := strings.NewReplacer(pairs...).Replace(pattern)

	// If starts with / after expansion, it's absolute
	if strings.HasPrefix(expanded, "/") {
		return expanded
	}

	// Otherwise, relative to hubPath
	return filepath.Join(ctx.HubPath, expanded)
}
