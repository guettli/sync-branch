package main

import (
	"os"

	"github.com/guettli/sync-branch/sync"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "sync-branch",
		Short: "Keep your local branch in sync with origin",
		Long: `sync-branch is a pre-commit hook that fetches origin
and merges any new commits from origin/<branch> and the repository's base
branch (e.g. main) into your local branch before every commit.`,
	}

	checkCmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch origin and merge any new commits into the current branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sync.Sync("")
		},
		SilenceUsage: true,
	}

	rootCmd.AddCommand(checkCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
