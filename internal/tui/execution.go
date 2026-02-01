package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/howell-aikit/aiflow/internal/config"
	"github.com/howell-aikit/aiflow/internal/executor"
	"github.com/howell-aikit/aiflow/internal/state"
)

// ExecutionModel handles the execution screen
type ExecutionModel struct {
	cfg   *config.Config
	run   *state.Run
	store *state.Store

	// UI state
	spinner   spinner.Model
	progress  progress.Model
	startTime time.Time

	// Execution state
	completed int
	total     int
	running   []*state.Task
	failed    []*state.Task
	outputs   map[string]string

	// Current output display
	currentTask    string
	currentOutput  []string
	maxOutputLines int

	// Done
	done bool
	err  error
}

// NewExecutionModel creates a new execution model
func NewExecutionModel(cfg *config.Config, run *state.Run, store *state.Store) ExecutionModel {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	return ExecutionModel{
		cfg:            cfg,
		run:            run,
		store:          store,
		spinner:        NewSpinner(),
		progress:       p,
		total:          len(run.Tasks),
		outputs:        make(map[string]string),
		maxOutputLines: 10,
	}
}

// Init initializes the execution model
func (m ExecutionModel) Init() tea.Cmd {
	m.startTime = time.Now()
	return tea.Batch(
		m.spinner.Tick,
		m.startExecution(),
	)
}

// Update handles messages
func (m ExecutionModel) Update(msg tea.Msg) (ExecutionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			// TODO: Cancel execution
			return m, nil
		}

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case taskStartedMsg:
		m.running = append(m.running, msg.task)
		m.currentTask = msg.task.ID
		return m, nil

	case taskOutputMsg:
		m.outputs[msg.taskID] = msg.output
		if msg.taskID == m.currentTask {
			lines := strings.Split(msg.output, "\n")
			if len(lines) > m.maxOutputLines {
				lines = lines[len(lines)-m.maxOutputLines:]
			}
			m.currentOutput = lines
		}
		return m, nil

	case taskCompletedMsg:
		m.removeRunning(msg.taskID)
		m.completed++

		if msg.err != nil {
			task := m.run.GetTask(msg.taskID)
			if task != nil {
				m.failed = append(m.failed, task)
			}
		}

		// Update progress
		percent := float64(m.completed) / float64(m.total)
		return m, m.progress.SetPercent(percent)

	case executionCompleteMsg:
		m.done = true
		m.err = msg.err
		if msg.err != nil {
			// Route to failure screen if we have task info, otherwise error screen
			if msg.failedTask != nil {
				return m, func() tea.Msg {
					return FailureTransitionMsg{
						FailedTask:  msg.failedTask,
						LastGoodSHA: msg.lastGoodSHA,
					}
				}
			}
			return m, func() tea.Msg {
				return ErrorMsg{Err: msg.err}
			}
		}
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenComplete}
		}
	}

	return m, nil
}

func (m *ExecutionModel) removeRunning(taskID string) {
	for i, t := range m.running {
		if t.ID == taskID {
			m.running = append(m.running[:i], m.running[i+1:]...)
			return
		}
	}
}

// View renders the execution screen
func (m ExecutionModel) View() string {
	var b strings.Builder

	// Header
	b.WriteString(titleStyle.Render("Executing Tasks"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(TruncateString(m.run.FeatureDesc, 60)))
	b.WriteString("\n\n")

	// Progress bar
	percent := float64(m.completed) / float64(m.total)
	b.WriteString(m.progress.ViewAs(percent))
	b.WriteString(fmt.Sprintf(" %d/%d", m.completed, m.total))
	b.WriteString("\n\n")

	// Elapsed time
	elapsed := time.Since(m.startTime).Round(time.Second)
	b.WriteString(dimStyle.Render(fmt.Sprintf("Elapsed: %s", elapsed)))
	b.WriteString("\n\n")

	// Task list
	b.WriteString("Tasks:\n")
	for _, task := range m.run.Tasks {
		icon := StatusIcon(task.Status)
		style := normalStyle
		if task.Status == state.TaskStatusRunning {
			style = infoStyle
		} else if task.Status == state.TaskStatusFailed {
			style = errorStyle
		} else if task.Status == state.TaskStatusCompleted {
			style = successStyle
		}

		line := fmt.Sprintf("  %s %s", icon, task.Title)
		b.WriteString(style.Render(line))

		if task.Status == state.TaskStatusRunning {
			b.WriteString(" ")
			b.WriteString(m.spinner.View())
		}
		b.WriteString("\n")
	}

	// Current output
	if len(m.currentOutput) > 0 && !m.done {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Output:"))
		b.WriteString("\n")
		outputBox := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			Width(60)
		b.WriteString(outputBox.Render(strings.Join(m.currentOutput, "\n")))
		b.WriteString("\n")
	}

	// Errors
	if len(m.failed) > 0 {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("Failed tasks:"))
		b.WriteString("\n")
		for _, task := range m.failed {
			b.WriteString(fmt.Sprintf("  - %s: %s\n", task.ID, task.Error))
		}
	}

	// Controls
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Ctrl+C to cancel"))

	return b.String()
}

// Commands and messages

type taskStartedMsg struct {
	task *state.Task
}

type taskOutputMsg struct {
	taskID string
	output string
}

type taskCompletedMsg struct {
	taskID string
	err    error
}

type executionCompleteMsg struct {
	err         error
	failedTask  *state.Task
	lastGoodSHA string
}

func (m ExecutionModel) startExecution() tea.Cmd {
	return func() tea.Msg {
		exec := executor.NewExecutor(m.cfg, m.run.WorktreePath, m.store, m.run)
		ctx := context.Background()
		err := exec.ExecuteAll(ctx, nil)

		// If error, find the failed task and last good commit
		var failedTask *state.Task
		var lastGoodSHA string

		if err != nil {
			// Reload run to get latest state
			if updatedRun, loadErr := m.store.LoadRun(m.run.ID); loadErr == nil {
				// Find failed task
				for _, t := range updatedRun.Tasks {
					if t.Status == state.TaskStatusFailed {
						failedTask = t
						break
					}
				}
				// Find last good commit from completed tasks
				for i := len(updatedRun.Tasks) - 1; i >= 0; i-- {
					t := updatedRun.Tasks[i]
					if t.Status == state.TaskStatusCompleted && t.CommitSHA != "" {
						lastGoodSHA = t.CommitSHA
						break
					}
				}
			}
		}

		return executionCompleteMsg{
			err:         err,
			failedTask:  failedTask,
			lastGoodSHA: lastGoodSHA,
		}
	}
}

// RunExecutor runs the actual executor (called from outside TUI)
func RunExecutor(cfg *config.Config, run *state.Run, store *state.Store) error {
	exec := executor.NewExecutor(cfg, run.WorktreePath, store, run)

	ctx := context.Background()
	return exec.ExecuteAll(ctx, func(completed, total int) {
		// Progress callback - could be used to update TUI
		fmt.Printf("Progress: %d/%d\n", completed, total)
	})
}

// ConfirmModel handles the task confirmation screen
type ConfirmModel struct {
	run          *state.Run
	store        *state.Store
	confirmed    bool
	selectedItem int
}

// NewConfirmModel creates a new confirm model
func NewConfirmModel(run *state.Run, store *state.Store) ConfirmModel {
	return ConfirmModel{
		run:   run,
		store: store,
	}
}

// Update handles messages for confirm model
func (m ConfirmModel) Update(msg tea.Msg) (ConfirmModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "enter":
			m.confirmed = true
			m.run.Status = state.RunStatusRunning
			if m.store != nil {
				m.store.SaveRun(m.run)
			}
			return m, func() tea.Msg {
				return ScreenTransitionMsg{Screen: ScreenExecution}
			}
		case "n", "q":
			return m, tea.Quit
		case "up", "k":
			if m.selectedItem > 0 {
				m.selectedItem--
			}
		case "down", "j":
			if m.selectedItem < len(m.run.Tasks)-1 {
				m.selectedItem++
			}
		}
	}
	return m, nil
}

// View renders the confirm screen
func (m ConfirmModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Confirm Execution"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Ready to execute %d tasks:\n\n", len(m.run.Tasks)))

	for i, task := range m.run.Tasks {
		prefix := "  "
		style := normalStyle
		if i == m.selectedItem {
			prefix = "> "
			style = selectedStyle
		}

		b.WriteString(style.Render(fmt.Sprintf("%s%d. %s", prefix, i+1, task.Title)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("y: confirm and execute  n: cancel"))

	return b.String()
}
