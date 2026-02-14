package cmd

import (
	"os"
	"strings"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

// completeBranchNames returns hub branch names for shell completion.
// Excludes the default branch (useful for remove).
func completeBranchNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	fs := afero.NewOsFs()
	cwd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for name := range hub.Config.Branches {
		if name == hub.Config.Repo.DefaultBranch {
			continue
		}
		if strings.HasPrefix(name, toComplete) {
			names = append(names, name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeRemoteBranchNames returns remote branch names not already in the hub.
func completeRemoteBranchNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	fs := afero.NewOsFs()
	g := git.New()
	cwd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	hubPath, err := hop.FindHub(fs, cwd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Resolve the default branch worktree path for running git commands
	dir := hubPath
	if defaultBranch, ok := hub.Config.Branches[hub.Config.Repo.DefaultBranch]; ok {
		dir = config.ResolveWorktreePath(defaultBranch.Path, hubPath)
	}

	remoteBranches, err := g.ListRemoteBranches(dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, branch := range remoteBranches {
		if _, exists := hub.Config.Branches[branch]; exists {
			continue
		}
		if strings.HasPrefix(branch, toComplete) {
			names = append(names, branch)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
