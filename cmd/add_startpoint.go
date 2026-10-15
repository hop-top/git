package cmd

import (
	"fmt"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// Start-point keywords add resolves itself, beside the WorktreeManager
// sentinels hop.StartPointDefaultBranch and hop.StartPointInitial.
const (
	// startPointCwd: the branch of the worktree add runs in; the default
	// branch when it runs outside every worktree of the repository.
	startPointCwd = "cwd"
	// startPointOriginDefault: origin's default branch (origin/<default>),
	// falling back to the local one; the default before cwd.
	startPointOriginDefault = "origin-default"
)

// addStartPoint is the start-point add resolved, and why.
type addStartPoint struct {
	// Spec is what WorktreeManager gets: a ref, a SHA, or one of its
	// sentinels.
	Spec string
	// Value is the setting's value: a keyword, a ref or a SHA.
	Value string
	// Origin names the setting that picked the start-point, as
	// "--from <v>", "GIT_HOP_ADD_FROM=<v>" or "hop.add.defaultStartPoint=<v>".
	Origin string
	// Why says what the setting resolved to, in words.
	Why string
	// Source is the worktree the start-point came from (cwd's), the
	// one whose ignored files seed the new worktree; "" leaves that to
	// resolveCopySource.
	Source string
}

// Describe renders the start-point and why it was picked, for the
// dry-run and verbose output.
func (s addStartPoint) Describe(defaultBranch string) string {
	what := describeStartPoint(s.Spec, defaultBranch)
	if s.Value == startPointOriginDefault && defaultBranch != "" {
		what = "'origin/" + defaultBranch + "'"
	}
	return fmt.Sprintf("%s (%s; %s)", what, s.Why, s.Origin)
}

// pickAddStartPoint applies the precedence --from > GIT_HOP_ADD_FROM >
// hop.add.defaultStartPoint (defaulted, so never empty in practice) and
// returns the value with the setting it came from.
func pickAddStartPoint(flagVal, envVal, configVal string) (value, origin string) {
	switch {
	case flagVal != "":
		return flagVal, "--from " + flagVal
	case envVal != "":
		return envVal, "GIT_HOP_ADD_FROM=" + envVal
	case configVal != "":
		return configVal, config.KeyAddDefaultStartPoint + "=" + configVal
	}
	return startPointCwd, config.KeyAddDefaultStartPoint + "=" + startPointCwd
}

// resolveAddStartPoint turns the configured start-point value into what
// WorktreeManager gets, for an add run in cwd of the hub at hubPath:
//
//   - cwd: the branch checked out in the worktree of this repository
//     cwd is in, as refs/heads/<branch>; outside every worktree, as
//     default-branch. On a detached HEAD, the commit it points at.
//   - default-branch: the local default branch, refs/heads/<default>;
//     refs/remotes/origin/<default> when there is no local one.
//   - origin-default: WorktreeManager's default-branch sentinel, which
//     prefers origin/<default> over the local branch.
//   - initial, or any ref or SHA: passed through.
//
// defaultBranch is the hub's repo.defaultBranch; when it is empty the
// name origin/HEAD points at stands in.
func resolveAddStartPoint(g git.GitInterface, hubPath, cwd, value, origin, defaultBranch string) (addStartPoint, error) {
	sp := addStartPoint{Spec: value, Value: value, Origin: origin}
	switch value {
	case startPointCwd:
		wt, branch, ok := cwdWorktree(g, hubPath, cwd)
		if !ok {
			sp = localDefaultStartPoint(g, hubPath, defaultBranch, value, origin)
			sp.Why = "not inside a worktree, so " + sp.Why
			return sp, nil
		}
		sp.Source = wt
		if branch == "" {
			// Like git switch -c and git worktree add -b: a detached
			// HEAD starts the branch at the commit it points at.
			out, err := g.RevParse(wt, "--verify", "HEAD^{commit}")
			if err != nil {
				return sp, fmt.Errorf("cannot read the detached HEAD of the worktree at %s: %v", wt, err)
			}
			sp.Spec = strings.TrimSpace(out)
			sp.Why = "the detached HEAD of the worktree at " + wt
			return sp, nil
		}
		sp.Spec = "refs/heads/" + branch
		sp.Why = "the current branch of the worktree at " + wt
		return sp, nil
	case hop.StartPointDefaultBranch:
		return localDefaultStartPoint(g, hubPath, defaultBranch, value, origin), nil
	case startPointOriginDefault:
		sp.Spec = hop.StartPointDefaultBranch
		sp.Why = "origin's default branch"
		return sp, nil
	case hop.StartPointInitial:
		sp.Why = "the root commit"
		return sp, nil
	}
	sp.Why = "named explicitly"
	return sp, nil
}

// localDefaultStartPoint is the default branch as a local ref, with the
// remote-tracking ref, then WorktreeManager's own fallback, behind it.
func localDefaultStartPoint(g git.GitInterface, hubPath, defaultBranch, value, origin string) addStartPoint {
	sp := addStartPoint{Spec: hop.StartPointDefaultBranch, Value: value, Origin: origin, Why: "the default branch"}
	def := effectiveDefaultBranch(g, hubPath, defaultBranch)
	switch {
	case def == "":
		sp.Why = "no default branch known"
	case refExists(g, hubPath, "refs/heads/"+def):
		sp.Spec = "refs/heads/" + def
	case refExists(g, hubPath, "refs/remotes/origin/"+def):
		sp.Spec = "refs/remotes/origin/" + def
		sp.Why = "the default branch, which exists only on origin"
	}
	return sp
}

// effectiveDefaultBranch is repo.defaultBranch, or the branch
// origin/HEAD names when hop.json records none.
func effectiveDefaultBranch(g git.GitInterface, hubPath, defaultBranch string) string {
	if defaultBranch != "" {
		return defaultBranch
	}
	out, err := g.RunInDir(hubPath, "git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "origin/")
}

// cwdWorktree reports the worktree of the hub's repository cwd is in,
// and the branch checked out there ("" for a detached HEAD). A cwd
// outside every working tree (the hub root of a bare hub, say), or in a
// working tree of another repository, reports false.
func cwdWorktree(g git.GitInterface, hubPath, cwd string) (path, branch string, ok bool) {
	hubRepo, ok := repositoryAt(g, hubPath)
	if !ok {
		return "", "", false
	}
	out, err := g.RunInDir(cwd, "git", "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !state.SamePath(strings.TrimSpace(lines[1]), hubRepo) {
		return "", "", false
	}
	path = strings.TrimSpace(lines[0])
	out, err = g.RunInDir(path, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return path, "", true
	}
	return path, strings.TrimSpace(out), true
}

// startPointBranch is the branch name a start-point spec names when it
// is one of add's refs/heads/<branch> forms, else the spec itself.
func startPointBranch(startPoint string) string {
	return strings.TrimPrefix(startPoint, "refs/heads/")
}

// resolveBranchBase normalizes the start-point string fed to
// WorktreeManager into the branch name to persist in HubBranch.Base.
// Returns "" when the base should NOT be recorded:
//   - empty / "default-branch" sentinel (= hub default; fallback handles it)
//   - "initial" sentinel (root commit, not a branch)
//   - input equal to the hub default branch (redundant with fallback)
//   - input that doesn't resolve to a local or remote-tracking branch ref
//     (raw SHA, tag, or a name that no longer exists)
//
// When the input resolves only via `refs/remotes/origin/<name>`, the
// returned base is the bare branch name (no `origin/` prefix) — that's
// the form used for comparison everywhere else.
func resolveBranchBase(g git.GitInterface, worktreePath, startPoint, defaultBranch string) string {
	startPoint = startPointBranch(startPoint)
	switch startPoint {
	case "", hop.StartPointDefaultBranch, hop.StartPointInitial:
		return ""
	}
	if startPoint == defaultBranch {
		return ""
	}
	if _, err := g.RevParse(worktreePath, "--verify", "refs/heads/"+startPoint); err == nil {
		return startPoint
	}
	if _, err := g.RevParse(worktreePath, "--verify", "refs/remotes/origin/"+startPoint); err == nil {
		return startPoint
	}
	return ""
}
