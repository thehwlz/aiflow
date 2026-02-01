package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/howell-aikit/aiflow/internal/config"
	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/pkg/git"
)

// FailureAction represents the user's choice on failure
type FailureAction int

const (
	ActionRetry FailureAction = iota
	ActionRollback
	ActionSkip
	ActionAbort
)

// FailureModel handles the task failure screen
type FailureModel struct {
	cfg          *config.Config
	run          *state.Run
	store        *state.Store
	failedTask   *state.Task
	lastGoodSHA  string
	selectedItem int
	actions      []FailureAction
	err          error
}

// NewFailureModel creates a new failure model
func NewFailureModel(cfg *config.Config, run *state.Run, store *state.Store, failedTask *state.Task, lastGoodSHA string) FailureModel {
	actions := []FailureAction{
		ActionRetry,
		ActionRollback,
		ActionSkip,
		ActionAbort,
	}

	// Remove rollback option if no previous commit available
	if lastGoodSHA == "" {
		actions = []FailureAction{
			ActionRetry,
			ActionSkip,
			ActionAbort,
		}
	}

	return FailureModel{
		cfg:         cfg,
		run:         run,
		store:       store,
		failedTask:  failedTask,
		lastGoodSHA: lastGoodSHA,
		actions:     actions,
	}
}

// Update handles messages for failure model
func (m FailureModel) Update(msg tea.Msg) (FailureModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedItem > 0 {
				m.selectedItem--
			}
		case "down", "j":
			if m.selectedItem < len(m.actions)-1 {
				m.selectedItem++
			}
		case "enter":
			return m.handleAction(m.actions[m.selectedItem])
		case "q", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m FailureModel) handleAction(action FailureAction) (FailureModel, tea.Cmd) {
	switch action {
	case ActionRetry:
		// Reset task status to pending and go back to execution
		m.failedTask.Status = state.TaskStatusPending
		m.failedTask.Error = ""
		m.store.UpdateTask(m.run.ID, m.failedTask.ID, func(t *state.Task) {
			t.Status = state.TaskStatusPending
			t.Error = ""
		})
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenExecution}
		}

	case ActionRollback:
		if m.lastGoodSHA != "" {
			repo, err := git.Open(m.run.WorktreePath)
			if err != nil {
				m.err = fmt.Errorf("failed to open repo: %w", err)
				return m, nil
			}
			if err := repo.ResetHard(m.lastGoodSHA); err != nil {
				m.err = fmt.Errorf("failed to rollback: %w", err)
				return m, nil
			}
		}
		// Reset task and go back to execution
		m.failedTask.Status = state.TaskStatusPending
		m.failedTask.Error = ""
		m.store.UpdateTask(m.run.ID, m.failedTask.ID, func(t *state.Task) {
			t.Status = state.TaskStatusPending
			t.Error = ""
		})
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenExecution}
		}

	case ActionSkip:
		// Mark task as completed (skipped) and continue
		m.store.UpdateTask(m.run.ID, m.failedTask.ID, func(t *state.Task) {
			t.Status = state.TaskStatusCompleted
			t.Error = "skipped"
		})
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenExecution}
		}

	case ActionAbort:
		m.run.Status = state.RunStatusFailed
		m.store.SaveRun(m.run)
		return m, tea.Quit
	}

	return m, nil
}

// View renders the failure screen
func (m FailureModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Task Failed"))
	b.WriteString("\n\n")

	if m.failedTask != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Task: %s", m.failedTask.Title)))
		b.WriteString("\n\n")

		if m.failedTask.Error != "" {
			b.WriteString(dimStyle.Render("Error:"))
			b.WriteString("\n")
			// Wrap and indent error message
			errLines := wrapText(m.failedTask.Error, 60)
			for _, line := range errLines {
				b.WriteString(dimStyle.Render("  " + line))
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Action error: %v", m.err)))
		b.WriteString("\n\n")
	}

	b.WriteString("What would you like to do?\n\n")

	actionLabels := map[FailureAction]string{
		ActionRetry:    "Retry task",
		ActionRollback: "Rollback to last commit",
		ActionSkip:     "Skip and continue",
		ActionAbort:    "Abort run",
	}

	actionDescs := map[FailureAction]string{
		ActionRetry:    "Try running the task again",
		ActionRollback: fmt.Sprintf("Reset to %s and retry", truncateSHA(m.lastGoodSHA)),
		ActionSkip:     "Mark as skipped and proceed with next task",
		ActionAbort:    "Stop execution and save current state",
	}

	for i, action := range m.actions {
		prefix := "  "
		style := normalStyle
		if i == m.selectedItem {
			prefix = "> "
			style = selectedStyle
		}

		label := actionLabels[action]
		b.WriteString(style.Render(fmt.Sprintf("%s%s", prefix, label)))
		b.WriteString("\n")

		if i == m.selectedItem {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    %s", actionDescs[action])))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓ select • Enter to confirm • q to quit"))

	return boxStyle.Render(b.String())
}

func truncateSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
