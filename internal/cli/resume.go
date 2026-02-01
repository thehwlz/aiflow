package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/internal/tui"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [run-id]",
	Short: "Resume an interrupted run",
	Long: `Resume an interrupted aiflow run.

This will:
1. Reset any tasks that were running to pending
2. Continue execution from where it left off
3. Use preserved summaries from completed tasks`,
	Args: cobra.MaximumNArgs(1),
	RunE: runResume,
}

func runResume(cmd *cobra.Command, args []string) error {
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

	// Check if run can be resumed
	switch run.Status {
	case state.RunStatusCompleted:
		return fmt.Errorf("run %s is already completed", run.ID)
	case state.RunStatusCancelled:
		fmt.Printf("Run %s was cancelled, resuming...\n", run.ID)
	case state.RunStatusFailed:
		fmt.Printf("Run %s had failures, resuming with failed tasks reset...\n", run.ID)
		// Reset failed tasks
		for _, t := range run.Tasks {
			if t.Status == state.TaskStatusFailed {
				t.Status = state.TaskStatusPending
				t.Error = ""
				t.StartedAt = nil
				t.CompletedAt = nil
			}
		}
	}

	// Reset running tasks to pending
	run.ResetRunningTasks()
	run.Status = state.RunStatusRunning

	if err := store.SaveRun(run); err != nil {
		return fmt.Errorf("failed to save run: %w", err)
	}

	// Set as current run
	if err := store.SetCurrentRun(run.ID); err != nil {
		return fmt.Errorf("failed to set current run: %w", err)
	}

	// Print status
	fmt.Printf("Resuming run: %s\n", run.ID)
	fmt.Printf("Feature: %s\n", run.FeatureDesc)
	fmt.Printf("Worktree: %s\n", run.WorktreePath)

	completed := len(run.GetCompletedTasks())
	ready := len(run.GetReadyTasks())
	pending := len(run.GetPendingTasks())

	fmt.Printf("\nProgress: %d completed, %d ready, %d pending\n", completed, ready, pending)

	// Print completed task summaries (useful for context)
	if completed > 0 {
		fmt.Printf("\nCompleted tasks with summaries:\n")
		for _, t := range run.Tasks {
			if t.Status == state.TaskStatusCompleted && t.Summary != nil {
				fmt.Printf("  - %s: %s\n", t.ID, t.Title)
				if len(t.Summary.Decisions) > 0 {
					fmt.Printf("    Decisions: %v\n", t.Summary.Decisions)
				}
			}
		}
	}

	// Print ready tasks
	if ready > 0 {
		fmt.Printf("\nReady to execute:\n")
		for _, t := range run.GetReadyTasks() {
			fmt.Printf("  - %s: %s\n", t.ID, t.Title)
		}
	}

	fmt.Printf("\nLaunching execution...\n")

	// Launch TUI at execution screen
	model := tui.NewModel(cfg, run, store)
	model.SetScreen(tui.ScreenExecution)
	p := tea.NewProgram(&model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
