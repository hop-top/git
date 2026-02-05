package cmd

import (
	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/shell"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var installShellIntegrationCmd = &cobra.Command{
	Use:   "install-shell-integration",
	Short: "Install shell wrapper for automatic directory switching",
	Long: `Installs a shell function wrapper that enables automatic directory
switching after git-hop commands. The wrapper is added to your shell's
configuration file (e.g., ~/.bashrc, ~/.zshrc, ~/.config/fish/config.fish).

The wrapper function:
  - Detects which git-hop commands should trigger directory changes
  - Automatically cd to the current worktree after successful operations
  - Preserves exit codes and command output

Supported shells: bash, zsh, fish`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()

		result, err := shell.InstallIntegration(fs)
		if err != nil {
			output.Fatal("Installation failed: %v", err)
		}

		output.Success("✓ Shell integration installed!")
		output.Info("")
		output.Info("Installed to: %s", result.RcPath)
		output.Info("Shell: %s", result.InstalledShell)
		output.Info("")
		output.Info("Restart your shell or run: source %s", result.RcPath)
		output.Info("")
		output.Info("You can now use: git-hop <branch>")
		output.Info("And it will automatically cd to the worktree.")
	},
}

func init() {
	cli.RootCmd.AddCommand(installShellIntegrationCmd)
}
