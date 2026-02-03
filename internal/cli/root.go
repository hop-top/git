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
	cfgFile      string
	jsonOut      bool
	porcelain    bool
	quiet        bool
	verbose      bool
	force        bool
	dryRun       bool
	gitDomain    string
	globalConfig bool

	// Build info
	version string
	commit  string
	date    string
)

func SetVersion(v, c, d string) {
	version = v
	commit = c
	date = d
	if RootCmd != nil {
		RootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
	}
}

var RootCmd *cobra.Command

// IsURI checks if a string is a URI (git@, http://, https://, or ends with .git)
func IsURI(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@") || strings.HasSuffix(s, ".git")
}

// ExpandShorthand converts "org/repo" to a full git URI
// Uses configured git domain (default: github.com)
func ExpandShorthand(s string, gitDomain string) string {
	// Already a full URI
	if IsURI(s) {
		return s
	}

	// Check if it looks like org/repo pattern
	parts := strings.Split(s, "/")
	if len(parts) == 2 && !strings.Contains(s, " ") {
		// Heuristic: org/repo patterns typically have more chars before the slash
		// Branch names like "feat/awesome" usually have 3-5 chars before slash
		// Org names are typically longer or at least look like identifiers
		// If the first part is very short (1-5 chars) and looks like a common
		// branch prefix, treat it as a branch name
		firstPart := parts[0]
		commonBranchPrefixes := []string{"feat", "fix", "bug", "docs", "test", "chore", "refactor", "perf", "style", "build", "ci", "revert"}
		for _, prefix := range commonBranchPrefixes {
			if firstPart == prefix {
				// This is likely a branch name, not org/repo
				return s
			}
		}

		// org/repo -> git@github.com:org/repo.git
		if gitDomain == "" {
			gitDomain = "github.com"
		}
		return fmt.Sprintf("git@%s:%s.git", gitDomain, s)
	}

	// Not a shorthand, return as-is (might be a branch name with more slashes)
	return s
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
		Version: "dev",
		Args:    cobra.ArbitraryArgs,
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

			// Load global config for defaults
			globalLoader := config.NewGlobalLoader()
			globalCfg, err := globalLoader.Load()

			// Get git domain from flag, config, or default
			domain := gitDomain
			if domain == "" && err == nil {
				domain = globalCfg.Defaults.GitDomain
			}
			if domain == "" {
				domain = "github.com"
			}

			// Expand shorthand notation (e.g., org/repo -> git@github.com:org/repo.git)
			expandedArg := ExpandShorthand(arg, domain)

			// Simple URI detection
			if IsURI(expandedArg) {
				// Check for --branch flag
				branch, _ := cmd.Flags().GetString("branch")

				if branch != "" {
					// Fork-Attach Mode (Inside Repo + URI + Branch)
					// Check if we are in a hub
					hubPath, err := hop.FindHub(fs, cwd)
					if err == nil {
						if err := hop.ForkAttach(fs, g, expandedArg, branch, hubPath); err != nil {
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
				useBare := true // Core default
				if err == nil {
					useBare = globalCfg.Defaults.BareRepo
				}

				if err := hop.CloneWorktree(fs, g, expandedArg, projectPath, useBare, globalConfig); err != nil {
					output.Fatal("Clone failed: %v", err)
				}
				return
			}

			// Branch-Attach Mode (Inside Repo + Branch Name)
			// Check if we're in or under a hub
			hubPath, err := hop.FindHub(fs, cwd)
			if err == nil {
				// Found a hub - check if worktree exists
				hub, loadErr := hop.LoadHub(fs, hubPath)
				if loadErr != nil {
					output.Fatal("Failed to load hub config: %v", loadErr)
				}

				// Check if this branch/worktree exists in the hub
				branch, exists := hub.Config.Branches[arg]
				if !exists {
					output.Fatal("Worktree '%s' does not exist. Use 'git hop add %s' to create it.", arg, arg)
				}

				// Switch to the worktree directory
				worktreePath := branch.Path
				if err := os.Chdir(worktreePath); err != nil {
					output.Fatal("Failed to change directory to worktree '%s': %v", worktreePath, err)
				}

				output.Success("Switched to worktree '%s'", arg)
				output.Info("Path: %s", worktreePath)
				return
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
	RootCmd.PersistentFlags().BoolVarP(&globalConfig, "global", "g", false, "use global hopspace in $GIT_HOP_DATA_HOME (default: local)")
	RootCmd.Flags().StringVar(&gitDomain, "git-domain", "", "Git domain for shorthand notation (e.g., github.com, gitlab.com)")
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
