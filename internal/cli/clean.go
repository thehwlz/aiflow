package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/internal/worktree"
	"github.com/howell-aikit/aiflow/pkg/git"
	"github.com/spf13/cobra"
)

var (
	cleanAll   bool
	cleanForce bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean [run-id...]",
	Short: "Remove run(s) and their worktrees",
	Long: `Remove aiflow run(s) and optionally their associated worktrees.

Examples:
  aiflow clean abc123      # Clean specific run
  aiflow clean --all       # Clean all runs
  aiflow clean -f abc123   # Force clean without confirmation`,
	RunE: runClean,
}

func init() {
	cleanCmd.Flags().BoolVarP(&cleanAll, "all", "a", false, "clean all runs and worktrees")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "skip confirmation")
}

func runClean(cmd *cobra.Command, args []string) error {
	store, err := state.NewStore(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	var runsToClean []*state.Run

	if cleanAll {
		runsToClean, err = store.ListRuns()
		if err != nil {
			return fmt.Errorf("failed to list runs: %w", err)
		}
	} else if len(args) > 0 {
		for _, id := range args {
			run, err := store.LoadRun(id)
			if err != nil {
				fmt.Printf("Warning: run %s not found\n", id)
				continue
			}
			runsToClean = append(runsToClean, run)
		}
	} else {
		return fmt.Errorf("specify run ID(s) or use --all")
	}

	if len(runsToClean) == 0 {
		fmt.Println("No runs to clean")
		return nil
	}

	// Confirm
	if !cleanForce {
		fmt.Printf("This will remove %d run(s) and their worktrees:\n", len(runsToClean))
		for _, run := range runsToClean {
			fmt.Printf("  - %s: %s\n", run.ID, run.FeatureDesc)
		}
		fmt.Print("\nContinue? [y/N] ")

		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Cancelled")
			return nil
		}
	}

	// Initialize worktree manager if we're in a repo
	var wtManager *worktree.Manager
	repoPath, err := git.FindRepoRootFromCwd()
	if err == nil {
		wtManager, _ = worktree.NewManager(repoPath, cfg.WorktreeDir)
	}

	// Clean runs
	for _, run := range runsToClean {
		// Remove worktree if it exists
		if wtManager != nil && run.WorktreePath != "" {
			if _, err := os.Stat(run.WorktreePath); err == nil {
				if err := wtManager.Remove(run.WorktreePath); err != nil {
					fmt.Printf("Warning: failed to remove worktree %s: %v\n", run.WorktreePath, err)
				} else {
					fmt.Printf("Removed worktree: %s\n", run.WorktreePath)
				}
			}
		}

		// Remove run state
		if err := store.DeleteRun(run.ID); err != nil {
			fmt.Printf("Warning: failed to remove run %s: %v\n", run.ID, err)
		} else {
			fmt.Printf("Removed run: %s\n", run.ID)
		}
	}

	fmt.Printf("\nCleaned %d run(s)\n", len(runsToClean))
	return nil
}
