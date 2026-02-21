package cmd

import (
	"os"
	"path/filepath"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
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
- Worktree state (orphaned directories)
- Orphaned worktrees in state

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

				// Check branch paths (worktrees)
				for name, b := range hub.Config.Branches {
					linkPath := filepath.Join(hub.Path, b.Path)
					if _, err := fs.Stat(linkPath); err != nil {
						output.Error("Broken link for branch %s: %s", name, linkPath)
						issuesFound = true

						if doctorFix {
							// Attempt to recreate the worktree
							output.Info("Attempting to fix broken worktree for branch %s...", name)

							// Get hopspace path for git worktree commands
							dataHome := hop.GetGitHopDataHome()
							hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

							// Load hopspace to verify it exists
							hopspace, err := hop.LoadHopspace(fs, hopspacePath)
							if err != nil {
								output.Error("Cannot fix: failed to load hopspace: %v", err)
								continue
							}

							// Check if branch is registered in hopspace
							_, existsInHopspace := hopspace.Config.Branches[b.HopspaceBranch]
							if !existsInHopspace {
								output.Error("Cannot fix: branch %s not found in hopspace", b.HopspaceBranch)
								continue
							}

							// Create parent directories if needed
							linkDir := filepath.Dir(linkPath)
							if err := fs.MkdirAll(linkDir, 0755); err != nil {
								output.Error("Failed to create parent directory: %v", err)
								continue
							}

							// Recreate the worktree using git
							g := git.New()
							if err := g.CreateWorktree(hopspacePath, b.HopspaceBranch, linkPath, "", false); err != nil {
								output.Error("Failed to recreate worktree: %v", err)
								continue
							}

							// Update hopspace to reflect the restored worktree
							if err := hopspace.RegisterBranch(b.HopspaceBranch, linkPath); err != nil {
								output.Error("Failed to update hopspace: %v", err)
								// Continue anyway as the worktree was created
							}

							// Verify the worktree was created successfully
							if _, err := fs.Stat(linkPath); err == nil {
								output.Info("✓ Fixed worktree for branch %s", name)
								fixedIssues++
							} else {
								output.Error("Worktree creation appeared to succeed but path still not accessible")
							}
						}
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
					worktrees := make(map[string]string, len(hub.Config.Branches))
					for branchName, branch := range hub.Config.Branches {
						worktrees[branchName] = filepath.Join(hubPath, branch.Path)
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

		// Check Worktree State
		output.Info("\n=== Checking Worktree State ===")
		if hubPath != "" {
			hub, err := hop.LoadHub(fs, hubPath)
			if err == nil {
				dataHome := hop.GetGitHopDataHome()
				hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

				// Load hopspace
				hopspace, err := hop.LoadHopspace(fs, hopspacePath)
				if err != nil {
					output.Error("Failed to load hopspace: %v", err)
					issuesFound = true
				} else {
					g := git.New()
					validator := hop.NewStateValidator(fs, g)
					cleanup := hop.NewCleanupManager(fs, g)

					// Check for orphaned directories
					orphanedDirs, err := validator.DetectOrphanedDirectories(hopspace)
					if err != nil {
						output.Error("Failed to detect orphaned directories: %v", err)
					} else if len(orphanedDirs) > 0 {
						issuesFound = true
						output.Error("Found %d orphaned directories", len(orphanedDirs))
						for _, dir := range orphanedDirs {
							output.Error("  - %s", dir)
							if doctorFix {
								output.Info("    Cleaning up...")
								fullPath := filepath.Join(hopspacePath, "hops", dir)
								if err := cleanup.CleanupOrphanedDirectory(fullPath); err != nil {
									output.Error("    Failed to remove: %v", err)
								} else {
									output.Info("    ✓ Removed")
									fixedIssues++
								}
							}
						}
						if !doctorFix {
							output.Info("  Run 'git hop doctor --fix' to clean up orphaned directories")
						}
					} else {
						output.Info("✓ No orphaned directories found")
					}
				}
			}
		} else {
			output.Info("Not in a hub. Skipping worktree state checks.")
		}

		// Check State Consistency
		output.Info("\n=== Checking State ===")
		st, err := state.LoadState(fs)
		if err != nil {
			output.Warn("Could not load state: %v", err)
			output.Info("Run 'git hop migrate' if you have legacy data to migrate.")
		} else if len(st.Repositories) > 0 {
			stateIssues := checkStateConsistency(fs, st)
			if len(stateIssues) > 0 {
				issuesFound = true
				output.Info("Found %d state consistency issue(s):", len(stateIssues))
				for _, issue := range stateIssues {
					output.Error("  %s", issue)
				}

				if doctorFix {
					output.Info("\nPruning orphaned entries from state...")
					// Use the prune functions
					worktreesPruned := pruneOrphanedWorktrees(fs, st)
					hubsPruned := pruneOrphanedHubs(fs, st)

					if worktreesPruned > 0 || hubsPruned > 0 {
						if err := state.SaveState(fs, st); err != nil {
							output.Error("Failed to save state: %v", err)
						} else {
							output.Info("✓ Pruned %d worktree(s) and %d hub(s)", worktreesPruned, hubsPruned)
							fixedIssues += worktreesPruned + hubsPruned
						}
					}
				} else {
					output.Info("\nRun 'git hop doctor --fix' or 'git hop prune' to clean up orphaned entries.")
				}
			} else {
				output.Info("✓ State is consistent")
			}
		} else {
			output.Info("No repositories in state. Skipping state checks.")
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

// checkStateConsistency verifies all worktrees in state exist on filesystem
func checkStateConsistency(fs afero.Fs, st *state.State) []string {
	var issues []string

	for repoID, repo := range st.Repositories {
		for branch, wt := range repo.Worktrees {
			if exists, _ := afero.DirExists(fs, wt.Path); !exists {
				issues = append(issues,
					"Worktree missing: "+repoID+":"+branch+" at "+wt.Path)
			}
		}
	}

	return issues
}
