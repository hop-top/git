package cmd

import (
	"os"

	"hop.top/git/internal/cli"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:       "completion [bash|zsh|fish]",
	Short:     "Generate shell completion scripts",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish"},
	Long: `Generate shell completion scripts for git-hop.

To load completions:

Bash:
  $ source <(git-hop completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ git-hop completion bash > /etc/bash_completion.d/git-hop
  # macOS:
  $ git-hop completion bash > $(brew --prefix)/etc/bash_completion.d/git-hop

Zsh:
  $ source <(git-hop completion zsh)

  # To load completions for each session, execute once:
  $ git-hop completion zsh > "${fpath[1]}/_git-hop"

Fish:
  $ git-hop completion fish | source

  # To load completions for each session, execute once:
  $ git-hop completion fish > ~/.config/fish/completions/git-hop.fish
`,
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			cli.RootCmd.GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			cli.RootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			cli.RootCmd.GenFishCompletion(os.Stdout, true)
		}
	},
}

func init() {
	cli.RootCmd.AddCommand(completionCmd)
}
