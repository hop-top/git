package cmd

import (
	"hop.top/git/internal/cli"
	"hop.top/git/internal/output"
	"hop.top/git/internal/shell"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var uninstallShellIntegrationCmd = &cobra.Command{
	Use:   "uninstall-shell-integration",
	Short: "Remove shell wrapper function",
	Long: `Removes the git-hop shell wrapper function from your shell's
configuration file. After uninstalling, you'll need to manually
navigate to worktree directories using cd.

This command:
  - Removes the wrapper function from your RC file
  - Updates configuration to mark integration as declined
  - Preserves any other content in your RC file`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()

		if err := shell.UninstallIntegration(fs); err != nil {
			output.Fatal("Uninstallation failed: %v", err)
		}

		output.Success("✓ Shell integration removed")
		output.Info("")
		output.Info("The wrapper function has been removed from your shell config.")
		output.Info("Restart your shell or run: source ~/.bashrc (or your shell's RC file)")
		output.Info("")
		output.Info("You can reinstall anytime with: git hop install-shell-integration")
	},
}

func init() {
	cli.RootCmd.AddCommand(uninstallShellIntegrationCmd)
}
