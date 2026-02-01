package cli

import (
	"fmt"
	"strings"

	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [run-id]",
	Short: "Show status of a run",
	Long:  `Show the current status of a run, including task progress and any errors.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	store, err := state.NewStore(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	var run *state.Run
	if len(args) > 0 {
		run, err = store.LoadRun(args[0])
	} else {
		run, err = store.GetCurrentRun()
		if err == nil && run == nil {
			return fmt.Errorf("no current run; specify a run ID or start a new run")
		}
	}
	if err != nil {
		return fmt.Errorf("failed to load run: %w", err)
	}

	// Print run info
	fmt.Printf("Run: %s\n", run.ID)
	fmt.Printf("Feature: %s\n", run.FeatureDesc)
	fmt.Printf("Status: %s\n", run.Status)
	fmt.Printf("Worktree: %s\n", run.WorktreePath)
	fmt.Printf("Base Branch: %s\n", run.BaseBranch)
	fmt.Printf("Created: %s\n", run.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated: %s\n", run.UpdatedAt.Format("2006-01-02 15:04:05"))

	if run.Error != "" {
		fmt.Printf("\nError: %s\n", run.Error)
	}

	// Print progress
	if len(run.Tasks) > 0 {
		completed := 0
		running := 0
		failed := 0
		pending := 0

		for _, t := range run.Tasks {
			switch t.Status {
			case state.TaskStatusCompleted:
				completed++
			case state.TaskStatusRunning:
				running++
			case state.TaskStatusFailed:
				failed++
			default:
				pending++
			}
		}

		fmt.Printf("\nProgress: %.0f%% (%d/%d tasks)\n", run.Progress(), completed, len(run.Tasks))
		fmt.Printf("  Completed: %d\n", completed)
		fmt.Printf("  Running: %d\n", running)
		fmt.Printf("  Pending: %d\n", pending)
		if failed > 0 {
			fmt.Printf("  Failed: %d\n", failed)
		}

		// Print task details
		fmt.Printf("\nTasks:\n")
		for _, t := range run.Tasks {
			statusIcon := getStatusIcon(t.Status)
			fmt.Printf("  %s [%s] %s\n", statusIcon, t.ID, t.Title)

			if t.Status == state.TaskStatusFailed && t.Error != "" {
				fmt.Printf("      Error: %s\n", t.Error)
			}

			if len(t.DependsOn) > 0 {
				fmt.Printf("      Depends on: %s\n", strings.Join(t.DependsOn, ", "))
			}
		}
	} else {
		fmt.Printf("\nNo tasks yet (breakdown not complete)\n")
	}

	return nil
}

func getStatusIcon(status state.TaskStatus) string {
	switch status {
	case state.TaskStatusCompleted:
		return "[x]"
	case state.TaskStatusRunning:
		return "[~]"
	case state.TaskStatusFailed:
		return "[!]"
	case state.TaskStatusReady:
		return "[>]"
	default:
		return "[ ]"
	}
}
