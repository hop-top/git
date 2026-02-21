package cmd

import (
	"os"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/output"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var installHooksCmd = &cobra.Command{
	Use:   "install-hooks",
	Short: "Install git-hop hooks in current repository",
	Long: `Install git-hop hooks in the current git worktree.

This command creates a .git-hop/hooks directory in the current worktree
where you can place repository-specific hook overrides.

Hooks follow a priority system:
  1. Repo override (.git-hop/hooks/<hook-name>)
  2. Hopspace hook ($XDG_DATA_HOME/git-hop/<org>/<repo>/hooks/<hook-name>)
  3. Global hook ($XDG_CONFIG_HOME/git-hop/hooks/<hook-name>)

Available hooks:
  - pre-worktree-add, post-worktree-add
  - pre-env-start, post-env-start
  - pre-env-stop, post-env-stop
`,
	Run: runInstallHooks,
}

func init() {
	cli.RootCmd.AddCommand(installHooksCmd)
}

func runInstallHooks(cmd *cobra.Command, args []string) {
	fs := afero.NewOsFs()
	g := git.New()

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		output.Fatal("Failed to get current directory: %v", err)
	}

	// Find git root
	root, err := g.GetRoot(cwd)
	if err != nil {
		output.Fatal("Not in a git worktree: %v", err)
	}

	// Install hooks
	hookRunner := hooks.NewRunner(fs)
	if err := hookRunner.InstallHooks(root); err != nil {
		output.Fatal("Failed to install hooks: %v", err)
	}

	output.Success("Hooks installed in: %s", root)
	output.Info("\nYou can now create hook scripts in: %s/.git-hop/hooks/", root)
	output.Info("\nAvailable hooks:")
	output.Info("  - pre-worktree-add, post-worktree-add")
	output.Info("  - pre-env-start, post-env-start")
	output.Info("  - pre-env-stop, post-env-stop")
}
