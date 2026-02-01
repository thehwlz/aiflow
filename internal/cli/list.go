package cli

import (
	"fmt"
	"path/filepath"

	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/internal/worktree"
	"github.com/howell-aikit/aiflow/pkg/git"
	"github.com/spf13/cobra"
)

var (
	listWorktrees bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all runs or worktrees",
	Long:  `List all aiflow runs and their status, or list worktrees with --worktrees flag.`,
	RunE:  runList,
}

func init() {
	listCmd.Flags().BoolVarP(&listWorktrees, "worktrees", "w", false, "list worktrees instead of runs")
}

func runList(cmd *cobra.Command, args []string) error {
	if listWorktrees {
		return listWorktreesFunc()
	}
	return listRunsFunc()
}

func listRunsFunc() error {
	store, err := state.NewStore(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	runs, err := store.ListRuns()
	if err != nil {
		return fmt.Errorf("failed to list runs: %w", err)
	}

	if len(runs) == 0 {
		fmt.Println("No runs found")
		return nil
	}

	// Get current run ID for highlighting
	currentID, _ := store.GetCurrentRunID()

	fmt.Printf("%-10s %-12s %-40s %s\n", "ID", "STATUS", "FEATURE", "PROGRESS")
	fmt.Printf("%-10s %-12s %-40s %s\n", "---", "------", "-------", "--------")

	for _, run := range runs {
		feature := run.FeatureDesc
		if len(feature) > 38 {
			feature = feature[:35] + "..."
		}

		progress := ""
		if len(run.Tasks) > 0 {
			progress = fmt.Sprintf("%.0f%% (%d/%d)",
				run.Progress(),
				len(run.GetCompletedTasks()),
				len(run.Tasks))
		}

		marker := " "
		if run.ID == currentID {
			marker = "*"
		}

		fmt.Printf("%s%-9s %-12s %-40s %s\n",
			marker,
			run.ID,
			run.Status,
			feature,
			progress)
	}

	fmt.Printf("\n* = current run\n")
	return nil
}

func listWorktreesFunc() error {
	repoPath, err := git.FindRepoRootFromCwd()
	if err != nil {
		return fmt.Errorf("must be in a git repository: %w", err)
	}

	wtManager, err := worktree.NewManager(repoPath, cfg.WorktreeDir)
	if err != nil {
		return fmt.Errorf("failed to initialize worktree manager: %w", err)
	}

	worktrees, err := wtManager.List()
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		fmt.Println("No worktrees found")
		return nil
	}

	fmt.Printf("%-40s %-30s %s\n", "ID", "BRANCH", "PATH")
	fmt.Printf("%-40s %-30s %s\n", "---", "------", "----")

	for _, wt := range worktrees {
		id := filepath.Base(wt.Path)
		if len(id) > 38 {
			id = id[:35] + "..."
		}

		branch := wt.Branch
		if len(branch) > 28 {
			branch = branch[:25] + "..."
		}

		fmt.Printf("%-40s %-30s %s\n", id, branch, wt.Path)
	}

	return nil
}
