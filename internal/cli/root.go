package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/cli"
	"hop.top/kit/upgrade"
	"hop.top/kit/xdg"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

var (
	cfgFile      string
	jsonOut      bool
	porcelain    bool
	quiet        bool
	verbose      bool
	force        bool
	dryRun       bool
	gitDomain    string
	globalConfig bool
	adminMode    bool

	version string
)

var Root *kitcli.Root

// RootCmd is the cobra root command — preserved for backward compat
// with cmd/*.go init() AddCommand calls.
var RootCmd *cobra.Command

func SetVersion(v, c, d string) {
	version = v
	if Root != nil {
		ver := fmt.Sprintf("%s (commit: %s, built: %s)", v, c, d)
		Root.Config.Version = ver
		if RootCmd != nil {
			RootCmd.Version = ver
		}
	}
}

func IsURI(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@") || strings.HasSuffix(s, ".git")
}

func ExpandShorthand(s string, gitDomain string) string {
	if IsURI(s) {
		return s
	}

	parts := strings.Split(s, "/")
	if len(parts) == 2 && !strings.Contains(s, " ") {
		firstPart := parts[0]
		commonBranchPrefixes := []string{"feat", "fix", "bug", "docs", "test", "chore", "refactor", "perf", "style", "build", "ci", "revert"}
		for _, prefix := range commonBranchPrefixes {
			if firstPart == prefix {
				return s
			}
		}

		if gitDomain == "" {
			gitDomain = "github.com"
		}
		return fmt.Sprintf("git@%s:%s.git", gitDomain, s)
	}

	return s
}

func Execute() error {
	return RootCmd.Execute()
}

func init() {
	Root = kitcli.New(kitcli.Config{
		Name:    "git-hop",
		Version: "dev",
		Short:   "Manage git worktrees and environments",
	})

	RootCmd = Root.Cmd
	RootCmd.Version = "dev"

	RootCmd.Long = `git-hop is a context-aware porcelain tool for managing
Git worktrees, Docker environments, and structured workspaces.

Clone Mode:
  git-hop <uri> [path]
  Clones a repository using bare repo + worktree structure (recommended)
  Configure default behavior via global config: bareRepo setting

Worktree Mode:
  git-hop <branch>
  Inside a project root: create/sync worktree for a branch`

	RootCmd.Args = cobra.ArbitraryArgs

	RootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		initConfig()
		setupOutputMode()
		if cmd.Name() != "upgrade" {
			upgrade.NotifyIfAvailable(cmd.Context(), newUpgradeChecker(), os.Stderr)
		}
	}

	RootCmd.Run = func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if adminMode {
				printAdminHelp(cmd)
				os.Exit(0)
			}
			cmd.Help()
			os.Exit(0)
		}

		arg := args[0]
		cwd, _ := os.Getwd()
		fs := afero.NewOsFs()
		g := git.New()

		globalLoader := config.NewGlobalLoader()
		globalCfg, err := globalLoader.Load()

		domain := gitDomain
		if domain == "" && err == nil {
			domain = globalCfg.Defaults.GitDomain
		}
		if domain == "" {
			domain = "github.com"
		}

		expandedArg := ExpandShorthand(arg, domain)

		if IsURI(expandedArg) {
			branch, _ := cmd.Flags().GetString("branch")

			if branch != "" {
				hubPath, err := hop.FindHub(fs, cwd)
				if err == nil {
					if err := hop.ForkAttach(fs, g, expandedArg, branch, hubPath); err != nil {
						output.Fatal("Fork-Attach failed: %v", err)
					}
					return
				}
			}

			projectPath := ""
			if len(args) > 1 {
				projectPath = args[1]
			}

			useBare := true
			if err == nil {
				useBare = globalCfg.Defaults.BareRepo
			}

			if err := hop.CloneWorktree(fs, g, expandedArg, projectPath, useBare, globalConfig); err != nil {
				output.Fatal("Clone failed: %v", err)
			}
			return
		}

		hubPath, err := hop.FindHub(fs, cwd)
		if err == nil {
			hub, loadErr := hop.LoadHub(fs, hubPath)
			if loadErr != nil {
				output.Fatal("Failed to load hub config: %v", loadErr)
			}

			branch, exists := hub.Config.Branches[arg]
			if !exists {
				output.Fatal("Worktree '%s' does not exist. Use 'git hop add %s' to create it.", arg, arg)
			}

			worktreePath := branch.Path
			if err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath); err != nil {
				output.Warn("Failed to update current symlink: %v", err)
			}

			if err := os.Chdir(worktreePath); err != nil {
				output.Fatal("Failed to change directory to worktree '%s': %v", worktreePath, err)
			}

			output.Success("Switched to worktree '%s'", arg)
			output.Info("Path: %s", worktreePath)
			return
		}

		output.Fatal("Unknown command or argument: %s", arg)
	}

	pf := RootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "", "config file (default is $XDG_CONFIG_HOME/git-hop/config.json)")
	pf.BoolVar(&jsonOut, "json", false, "output in JSON format")
	pf.BoolVar(&porcelain, "porcelain", false, "machine-readable output")
	// --quiet is already registered by kit/cli.New(); add -q shorthand
	if f := pf.Lookup("quiet"); f != nil {
		f.Shorthand = "q"
	}
	pf.BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	pf.BoolVar(&force, "force", false, "bypass safety checks")
	pf.BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	pf.BoolVarP(&globalConfig, "global", "g", false, "use global hopspace in $GIT_HOP_DATA_HOME (default: local)")

	RootCmd.Flags().StringVar(&gitDomain, "git-domain", "", "Git domain for shorthand notation (e.g., github.com, gitlab.com)")
	RootCmd.Flags().String("branch", "", "branch name for fork-attach mode")

	RootCmd.Flags().BoolVar(&adminMode, "admin", false, "")
	RootCmd.Flags().MarkHidden("admin")

	_ = Root.Viper.BindPFlag("json", pf.Lookup("json"))
	_ = Root.Viper.BindPFlag("verbose", pf.Lookup("verbose"))
}

func printAdminHelp(cmd *cobra.Command) {
	fmt.Println("Admin commands:")
	fmt.Println()
	for _, sub := range cmd.Commands() {
		if sub.Hidden && sub.Name() != "" {
			fmt.Printf("  %-20s %s\n", sub.Name(), sub.Short)
		}
	}
}

func initConfig() {
	v := Root.Viper
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		configDir, err := xdg.ConfigDir("git-hop")
		if err != nil {
			configDir = filepath.Join(os.Getenv("HOME"), ".config", "git-hop")
		}
		v.AddConfigPath(configDir)
		v.SetConfigName("config")
		v.SetConfigType("json")
	}

	v.SetEnvPrefix("GIT_HOP")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err == nil && verbose {
		output.Debug("using config file: %s", v.ConfigFileUsed())
	}
}

func setupOutputMode() {
	quiet = Root.Viper.GetBool("quiet")

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

	output.SetViper(Root.Viper)
	output.SetupLogger(mode, verbose)
}
