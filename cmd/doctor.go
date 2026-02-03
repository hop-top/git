package cmd

import (
	"os"
	"path/filepath"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	doctorFix bool
)

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Aliases: []string{"check", "repair"},
	Short:   "Check and repair the environment",
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

		// Check Dependencies
		output.Info("\n=== Checking Dependencies ===")
		if hubPath != "" {
			hub, err := hop.LoadHub(fs, hubPath)
			if err == nil {
				dataHome := hop.GetGitHopDataHome()
				hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

				// Load global config
				globalLoader := config.NewGlobalLoader()
				globalConfig, err := globalLoader.Load()
				if err != nil {
					globalConfig = globalLoader.GetDefaults()
				}

				// Create deps manager
				depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
				if err != nil {
					output.Error("Failed to initialize dependency manager: %v", err)
					issuesFound = true
				} else {
					// Collect all worktree paths
					worktrees := make([]string, 0, len(hub.Config.Branches))
					for _, branch := range hub.Config.Branches {
						worktreePath := filepath.Join(hubPath, branch.Path)
						worktrees = append(worktrees, worktreePath)
					}

					// Run audit
					issues, err := depsManager.Audit(worktrees)
					if err != nil {
						output.Error("Failed to audit dependencies: %v", err)
						issuesFound = true
					} else if len(issues) > 0 {
						issuesFound = true
						output.Info("\nDependency Issues:")

						var totalReclaimableSize int64
						for _, issue := range issues {
							switch issue.Type {
							case services.IssueLocalFolder:
								sizeMB := float64(issue.Size) / 1024 / 1024
								output.Error("  ⚠ %s: local %s (%.1fMB) instead of symlink", issue.Branch, issue.PM.DepsDir, sizeMB)
								totalReclaimableSize += issue.Size
							case services.IssueBrokenSymlink:
								output.Error("  ✗ %s: broken symlink %s → %s (missing)", issue.Branch, issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget))
							case services.IssueStaleSymlink:
								output.Error("  ⚠ %s: stale symlink %s → %s (lockfile changed to %s)", issue.Branch, issue.PM.DepsDir, filepath.Base(issue.SymlinkTarget), issue.ExpectedHash[:6])
							case services.IssueMissingDeps:
								output.Error("  ✗ %s: missing %s", issue.Branch, issue.PM.DepsDir)
							}
						}

						if totalReclaimableSize > 0 {
							sizeMB := float64(totalReclaimableSize) / 1024 / 1024
							output.Info("\nPotential space savings: %.1fMB", sizeMB)
						}

						if doctorFix {
							output.Info("\nFixing dependency issues...")
							if err := depsManager.Fix(issues, false); err != nil {
								output.Error("Failed to fix some issues: %v", err)
							} else {
								output.Info("✓ Fixed %d dependency issue(s)", len(issues))
								fixedIssues += len(issues)

								if totalReclaimableSize > 0 {
									sizeMB := float64(totalReclaimableSize) / 1024 / 1024
									output.Info("✓ Reclaimed %.1fMB", sizeMB)
								}
							}
						}

						// Check for orphaned deps
						orphaned := depsManager.Registry.GetOrphaned()
						if len(orphaned) > 0 {
							var orphanedSize int64
							for _, depsKey := range orphaned {
								depsPath := filepath.Join(hopspacePath, "deps", depsKey)
								size := getDirSize(fs, depsPath)
								orphanedSize += size
							}
							orphanedSizeMB := float64(orphanedSize) / 1024 / 1024
							output.Info("\n  ⚠ %d orphaned dependencies (%.1fMB)", len(orphaned), orphanedSizeMB)
							output.Info("    Run 'git hop env gc' to reclaim space")
						}
					} else {
						output.Info("✓ All dependencies are properly configured")

						// Still check for orphaned deps
						orphaned := depsManager.Registry.GetOrphaned()
						if len(orphaned) > 0 {
							var orphanedSize int64
							for _, depsKey := range orphaned {
								depsPath := filepath.Join(hopspacePath, "deps", depsKey)
								size := getDirSize(fs, depsPath)
								orphanedSize += size
							}
							orphanedSizeMB := float64(orphanedSize) / 1024 / 1024
							output.Info("  ⚠ %d orphaned dependencies (%.1fMB)", len(orphaned), orphanedSizeMB)
							output.Info("    Run 'git hop env gc' to reclaim space")
						}
					}
				}
			}
		} else {
			output.Info("Not in a hub. Skipping dependency checks.")
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

// getDirSize calculates the total size of a directory
func getDirSize(fs afero.Fs, path string) int64 {
	var size int64
	afero.Walk(fs, path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
