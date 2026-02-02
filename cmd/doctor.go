package cmd

import (
	"os"
	"path/filepath"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	doctorFix bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check and repair the environment",
	Long: `Run diagnostics on git-hop installation and project setup.

Checks:
- Path configuration (data home, config home, cache home)
- Hub configuration and symlinks
- Hopspace existence and consistency
- Orphaned worktrees

Use --fix to automatically repair issues.`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		output.Info("Running git-hop diagnostics...")
		issuesFound := false
		fixedIssues := 0

		// Check Paths
		output.Info("\n=== Checking Paths ===")
		dataHome := hop.GetGitHopDataHome()
		configHome := hop.GetConfigHome()
		cacheHome := hop.GetCacheHome()

		output.Info("Data home:   %s", dataHome)
		output.Info("Config home: %s", configHome)
		output.Info("Cache home:  %s", cacheHome)

		// Verify directories exist
		for _, dir := range []struct {
			name string
			path string
		}{
			{"data", filepath.Join(dataHome, "git-hop")},
			{"config", filepath.Join(configHome, "git-hop")},
			{"cache", filepath.Join(cacheHome, "git-hop")},
		} {
			if exists, _ := afero.DirExists(fs, dir.path); !exists {
				issuesFound = true
				if doctorFix {
					if err := fs.MkdirAll(dir.path, 0755); err != nil {
						output.Error("Failed to create %s directory: %v", dir.name, err)
					} else {
						output.Info("✓ Created %s directory", dir.name)
						fixedIssues++
					}
				} else {
					output.Error("%s directory does not exist: %s", dir.name, dir.path)
				}
			}
		}

		// Check Hub
		output.Info("\n=== Checking Hub ===")
		hubPath, err := hop.FindHub(fs, cwd)
		if err == nil {
			output.Info("Hub found at: %s", hubPath)
			hub, err := hop.LoadHub(fs, hubPath)
			if err != nil {
				output.Error("Failed to load hub config: %v", err)
				issuesFound = true
			} else {
				// Check if hopspace exists
				dataHome := hop.GetGitHopDataHome()
				hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

				output.Info("Expected hopspace: %s", hopspacePath)

				if exists, _ := afero.Exists(fs, filepath.Join(hopspacePath, "hop.json")); !exists {
					issuesFound = true

					if doctorFix {
						output.Info("Creating missing hopspace...")
						// Get the default branch
						defaultBranch := hub.Config.Repo.DefaultBranch
						if defaultBranch == "" {
							defaultBranch = "main"
						}

						// Initialize hopspace
						hopspace, err := hop.InitHopspace(fs, hopspacePath, hub.Config.Repo.URI,
							hub.Config.Repo.Org, hub.Config.Repo.Repo, defaultBranch)
						if err != nil {
							output.Error("Failed to initialize hopspace: %v", err)
						} else {
							// Register all branches from hub
							for branchName, branch := range hub.Config.Branches {
								branchWorktreePath := filepath.Join(hubPath, branch.Path)
								if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
									output.Error("Failed to register branch %s: %v", branchName, err)
								}
							}
							output.Info("✓ Created hopspace")
							fixedIssues++
						}
					} else {
						output.Error("Hopspace does not exist at %s", hopspacePath)
					}
				} else {
					output.Info("✓ Hopspace exists")

					// Check consistency between hub and hopspace
					hopspace, err := hop.LoadHopspace(fs, hopspacePath)
					if err != nil {
						output.Error("Failed to load hopspace: %v", err)
						issuesFound = true
					} else {
						// Check if all hub branches are in hopspace
						for branchName := range hub.Config.Branches {
							if _, ok := hopspace.Config.Branches[branchName]; !ok {
								issuesFound = true

								if doctorFix {
									branchWorktreePath := filepath.Join(hubPath, hub.Config.Branches[branchName].Path)
									if err := hopspace.RegisterBranch(branchName, branchWorktreePath); err != nil {
										output.Error("Failed to register branch %s: %v", branchName, err)
									} else {
										output.Info("✓ Registered branch %s in hopspace", branchName)
										fixedIssues++
									}
								} else {
									output.Error("Branch %s in hub but not in hopspace", branchName)
								}
							}
						}
					}
				}

				// Check symlinks
				for name, b := range hub.Config.Branches {
					linkPath := filepath.Join(hub.Path, b.Path)
					if _, err := fs.Stat(linkPath); err != nil {
						output.Error("Broken link for branch %s: %s", name, linkPath)
						issuesFound = true
						// TODO: Add fix logic for broken symlinks
					}
				}
			}
		} else {
			output.Info("Not in a hub. Skipping hub-specific checks.")
		}

		// Summary
		output.Info("\n=== Summary ===")
		if !issuesFound {
			output.Info("✓ No issues found. Your git-hop installation is healthy!")
		} else {
			if doctorFix {
				if fixedIssues > 0 {
					output.Info("Fixed %d issue(s).", fixedIssues)
				}
				if issuesFound {
					output.Info("Some issues could not be automatically fixed. Please review the errors above.")
				}
			} else {
				output.Info("Issues found. Run 'git hop doctor --fix' to automatically repair them.")
			}
		}
	},
}

func init() {
	cli.RootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Automatically fix issues")
}
