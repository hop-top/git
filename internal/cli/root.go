package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Global flags
	cfgFile   string
	jsonOut   bool
	porcelain bool
	quiet     bool
	verbose   bool
	force     bool
	dryRun    bool

	// Build info
	versionStr string
)

func SetVersionInfo(v, c, d string) {
	versionStr = fmt.Sprintf("git-hop version %s (%s built %s)", v, c, d)
}

var RootCmd *cobra.Command

func isURI(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@") || strings.HasSuffix(s, ".git")
}

func Execute() error {
	return RootCmd.Execute()
}

func init() {
	RootCmd = &cobra.Command{
		Use:   "git-hop",
		Short: "Manage git worktrees and environments",
		Long: `git-hop is a context-aware porcelain tool for managing
Git worktrees, Docker environments, and structured workspaces.

Clone Mode:
  git-hop <uri> [path]
  Clones a repository using bare repo + worktree structure (recommended)
  Configure default behavior via global config: bareRepo setting

Worktree Mode:
  git-hop <branch>
  Inside a project root: create/sync worktree for a branch`,
		Args: cobra.ArbitraryArgs,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			initConfig()
			setupOutputMode()
		},
		// If no subcommand, we fall through (core dispatch logic comes later)
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				cmd.Help()
				os.Exit(0)
			}

			arg := args[0]
			cwd, _ := os.Getwd()
			fs := afero.NewOsFs()
			g := git.New()

			// Simple URI detection
			if isURI(arg) {
				// Check for --branch flag
				branch, _ := cmd.Flags().GetString("branch")

				if branch != "" {
					// Fork-Attach Mode (Inside Repo + URI + Branch)
					// Check if we are in a hub
					hubPath, err := hop.FindHub(fs, cwd)
					if err == nil {
						if err := hop.ForkAttach(fs, g, arg, branch, hubPath); err != nil {
							output.Fatal("Fork-Attach failed: %v", err)
						}
						return
					}
				}

				// Clone Mode - use new bare repo + worktree approach
				projectPath := ""
				if len(args) > 1 {
					projectPath = args[1]
				}

				// Use bare repo setting from global config (default: true)
				globalLoader := config.NewGlobalLoader()
				globalCfg, err := globalLoader.Load()
				useBare := true // Core default
				if err == nil {
					useBare = globalCfg.Defaults.BareRepo
				}

				if err := hop.CloneWorktree(fs, g, arg, projectPath, useBare); err != nil {
					output.Fatal("Clone failed: %v", err)
				}
				return
			}

			// Branch-Attach Mode (Inside Repo + Branch Name)
			// Check if we're in or under a hub
			hubPath, err := hop.FindHub(fs, cwd)
			if err == nil {
				// Found a hub - delegate to Add command
				addCmd, _, _ := cmd.Find([]string{"add"})
				if addCmd != nil {
					// Change to hub directory for add command
					origDir := cwd
					os.Chdir(hubPath)
					defer os.Chdir(origDir)

					addCmd.Run(addCmd, []string{arg})
					return
				}
			}

			output.Fatal("Unknown command or argument: %s", arg)
		},
	}

	// Global Flags
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $XDG_CONFIG_HOME/git-hop/config.json)")
	RootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output in JSON format")
	RootCmd.PersistentFlags().BoolVar(&porcelain, "porcelain", false, "machine-readable output")
	RootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress output")
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	RootCmd.PersistentFlags().BoolVar(&force, "force", false, "bypass safety checks")
	RootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	RootCmd.Flags().String("branch", "", "branch name for fork-attach mode")

	// Bind Viper to flags (optional, but good for config/env overrides)
	viper.BindPFlag("json", RootCmd.PersistentFlags().Lookup("json"))
	viper.BindPFlag("verbose", RootCmd.PersistentFlags().Lookup("verbose"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Default to XDG_CONFIG_HOME
		configDir, err := os.UserConfigDir()
		if err != nil {
			configDir = "$HOME/.config"
		}
		viper.AddConfigPath(filepath.Join(configDir, "git-hop"))
		viper.SetConfigName("config")
		viper.SetConfigType("json")
	}

	viper.SetEnvPrefix("GIT_HOP")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil && verbose {
		fmt.Fprintln(os.Stderr, "debug: using config file:", viper.ConfigFileUsed())
	}
}

func setupOutputMode() {
	var mode output.Mode
	if jsonOut {
		mode = output.ModeJSON
	} else if porcelain {
		mode = output.ModePorcelain
	} else if quiet {
		mode = output.ModeQuiet
	} else {
		mode = output.ModeHuman
	}

	// Setup the logger with charmbracelet/log
	output.SetupLogger(mode, verbose)
}
