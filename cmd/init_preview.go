package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/output"
	"hop.top/git/internal/shell"
)

// initDryRunBanner opens every init preview.
const initDryRunBanner = "DRY RUN - No changes will be made"

// previewInitFinish reports the closing steps an init run would take,
// in its order, without taking them: the hooks directory (unless
// --no-hooks), the mirror of committed hooks into the hopspace, and the
// shell integration (--enable-chdir).
//
// hooksAt is the worktree the hooks directory goes in; contentAt is
// where that worktree's files are now. They differ only before a bare
// conversion, which moves the checkout into hops/<branch>.
func previewInitFinish(fs afero.Fs, g git.GitInterface, hooksAt, contentAt, repoPath string, noHooks, enableChdir bool) {
	if !noHooks {
		dir := filepath.Join(hooksAt, ".git-hop", "hooks")
		if exists, _ := afero.DirExists(fs, filepath.Join(contentAt, ".git-hop", "hooks")); exists {
			fmt.Printf("Hooks directory: %s/ (present)\n", dir)
		} else {
			fmt.Printf("Would create hooks directory: %s/\n", dir)
		}
	}
	previewMirrorInitHooks(fs, g, contentAt, repoPath, noHooks)
	if enableChdir {
		previewShellIntegration()
	}
}

// previewMirrorInitHooks reports the hooks mirrorInitHooks would mirror
// into the hopspace, from the same decisions, writing and asking nothing.
func previewMirrorInitHooks(fs afero.Fs, g git.GitInterface, worktreePath, repoPath string, noHooks bool) {
	mopts, ok := initMirrorOpts(g, worktreePath, repoPath, initHooksMode, initHooksOverwrite, noHooks)
	if !ok {
		return
	}
	mopts.DryRun = true
	res, err := hooks.MirrorCommittedHooks(fs, mopts)
	if err != nil {
		output.Warn("failed to mirror committed hooks: %v", err)
		return
	}
	for _, h := range res.Hooks {
		switch h.Status {
		case "would-install":
			fmt.Printf("Would mirror hook %s into %s (%s)\n", h.Name, h.Target, mopts.Mode)
		case "would-prompt":
			fmt.Printf("Would ask whether to mirror hook %s into %s\n", h.Name, h.Target)
		}
	}
}

// previewShellIntegration reports the rc file --enable-chdir would write,
// or warns as a real run would when it cannot.
func previewShellIntegration() {
	_, rcPath, err := shell.IntegrationTarget()
	if err != nil {
		output.Warn("failed to install shell integration: %v", err)
		return
	}
	fmt.Printf("Would install shell integration into %s and record it in the global git config\n", rcPath)
}

// previewRegisterAsIs is registerAsIs's dry run: the registration it
// would record and the closing steps it would take.
func previewRegisterAsIs(fs afero.Fs, g git.GitInterface, org, repo, branch, repoPath string, noHooks, enableChdir bool) {
	fmt.Println(initDryRunBanner)
	fmt.Println("Would register the repository as-is, in the global registry and state:")
	fmt.Printf("  Repo: %s/%s\n", org, repo)
	fmt.Printf("  Branch: %s\n", branch)
	fmt.Printf("  Path: %s\n", repoPath)
	previewInitFinish(fs, g, repoPath, repoPath, repoPath, noHooks, enableChdir)
	output.Hint("To register it, run %s and choose 3", initProceedCommand(initRunFlags))
}

// previewAlreadyInitialized is handleAlreadyInitializedWithFlags's dry
// run: the hop.json back-fill and the closing steps it would take.
func previewAlreadyInitialized(fs afero.Fs, g git.GitInterface, path string, structure config.StructureType, noHooks, enableChdir bool) {
	fmt.Println(initDryRunBanner)
	if hubPath, ok := resolveBackfillRoot(fs, g, path, structure); ok {
		if exists, _ := afero.Exists(fs, filepath.Join(hubPath, "hop.json")); !exists {
			fmt.Printf("Would create missing hop.json at %s and register the hub\n", hubPath)
		}
	}
	printAlreadyInitialized(path, structure)
	previewInitFinish(fs, g, path, path, path, noHooks, enableChdir)
}
