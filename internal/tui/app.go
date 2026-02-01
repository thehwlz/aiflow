package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/howell-aikit/aiflow/internal/config"
	"github.com/howell-aikit/aiflow/internal/state"
)

// Styles for the TUI
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginBottom(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(1, 2)
)

// Screen represents different screens in the TUI
type Screen int

const (
	ScreenBreakdown Screen = iota
	ScreenConfirm
	ScreenExecution
	ScreenComplete
	ScreenFailure
	ScreenError
)

// Model is the main TUI model
type Model struct {
	// Configuration
	cfg   *config.Config
	run   *state.Run
	store *state.Store

	// Current screen
	screen Screen

	// Sub-models
	breakdown  BreakdownModel
	confirm    ConfirmModel
	execution  ExecutionModel
	failure    FailureModel
	completion CompletionModel

	// State
	err      error
	quitting bool

	// Dimensions
	width  int
	height int
}

// NewModel creates a new TUI model
func NewModel(cfg *config.Config, run *state.Run, store *state.Store) Model {
	return Model{
		cfg:        cfg,
		run:        run,
		store:      store,
		screen:     ScreenBreakdown,
		breakdown:  NewBreakdownModel(cfg, run, store),
		confirm:    NewConfirmModel(run, store),
		execution:  NewExecutionModel(cfg, run, store),
		completion: NewCompletionModel(cfg, run, store),
	}
}

// SetScreen sets the current screen
func (m *Model) SetScreen(screen Screen) {
	m.screen = screen
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	switch m.screen {
	case ScreenBreakdown:
		return m.breakdown.Init()
	case ScreenExecution:
		return m.execution.Init()
	default:
		return nil
	}
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.screen != ScreenExecution {
				m.quitting = true
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case ScreenTransitionMsg:
		m.screen = msg.Screen
		switch m.screen {
		case ScreenExecution:
			// Reload run from store to get latest task status
			if m.store != nil {
				if updatedRun, err := m.store.LoadRun(m.run.ID); err == nil {
					m.run = updatedRun
				}
			}
			m.execution = NewExecutionModel(m.cfg, m.run, m.store)
			return m, m.execution.Init()
		case ScreenComplete:
			m.completion = NewCompletionModel(m.cfg, m.run, m.store)
			return m, nil
		}
		return m, nil

	case FailureTransitionMsg:
		m.screen = ScreenFailure
		m.failure = NewFailureModel(m.cfg, m.run, m.store, msg.FailedTask, msg.LastGoodSHA)
		return m, nil

	case ErrorMsg:
		m.err = msg.Err
		m.screen = ScreenError
		return m, nil
	}

	// Delegate to current screen
	var cmd tea.Cmd
	switch m.screen {
	case ScreenBreakdown:
		m.breakdown, cmd = m.breakdown.Update(msg)
	case ScreenConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	case ScreenExecution:
		m.execution, cmd = m.execution.Update(msg)
	case ScreenFailure:
		m.failure, cmd = m.failure.Update(msg)
	case ScreenComplete:
		m.completion, cmd = m.completion.Update(msg)
	}

	return m, cmd
}

// View renders the model
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var content string
	switch m.screen {
	case ScreenBreakdown:
		content = m.breakdown.View()
	case ScreenConfirm:
		content = m.confirm.View()
	case ScreenExecution:
		content = m.execution.View()
	case ScreenComplete:
		content = m.completion.View()
	case ScreenFailure:
		content = m.failure.View()
	case ScreenError:
		content = m.viewError()
	}

	return content
}

func (m Model) viewError() string {
	errMsg := "Unknown error"
	if m.err != nil {
		errMsg = m.err.Error()
	}

	return boxStyle.Render(
		errorStyle.Render("Error") + "\n\n" +
			errMsg + "\n\n" +
			dimStyle.Render("Press q to quit"),
	)
}

// Messages

// ScreenTransitionMsg transitions to a new screen
type ScreenTransitionMsg struct {
	Screen Screen
}

// FailureTransitionMsg transitions to failure screen with context
type FailureTransitionMsg struct {
	FailedTask  *state.Task
	LastGoodSHA string
}

// ErrorMsg indicates an error occurred
type ErrorMsg struct {
	Err error
}

// Helper functions

// StatusIcon returns an icon for task status
func StatusIcon(status state.TaskStatus) string {
	switch status {
	case state.TaskStatusCompleted:
		return successStyle.Render("✓")
	case state.TaskStatusRunning:
		return infoStyle.Render("●")
	case state.TaskStatusFailed:
		return errorStyle.Render("✗")
	case state.TaskStatusReady:
		return warningStyle.Render("○")
	default:
		return dimStyle.Render("○")
	}
}

// TruncateString truncates a string to maxLen
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// Run starts the TUI
func Run(cfg *config.Config, run *state.Run, store *state.Store) error {
	p := tea.NewProgram(
		NewModel(cfg, run, store),
		tea.WithAltScreen(),
	)

	_, err := p.Run()
	return err
}

// NewSpinner creates a spinner with default style
func NewSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return s
}
