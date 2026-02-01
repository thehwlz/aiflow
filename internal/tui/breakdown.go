package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/howell-aikit/aiflow/internal/breakdown"
	"github.com/howell-aikit/aiflow/internal/state"
)

// BreakdownPhase represents the current phase of breakdown
type BreakdownPhase int

const (
	PhaseAnalyzing BreakdownPhase = iota
	PhaseQuestions
	PhaseBreakingDown
	PhaseReview
)

// BreakdownModel handles the breakdown screen
type BreakdownModel struct {
	run       *state.Run
	phase     BreakdownPhase
	spinner   spinner.Model
	breakdown *breakdown.Breakdown

	// Question handling
	questions       []breakdown.ClarificationQuestion
	currentQuestion int
	textInput       textinput.Model
	selectedOption  int

	// Task review
	tasks         []*state.Task
	selectedTask  int
	scrollOffset  int

	// Output
	output string
	err    error
}

// NewBreakdownModel creates a new breakdown model
func NewBreakdownModel(run *state.Run) BreakdownModel {
	ti := textinput.New()
	ti.Placeholder = "Enter your answer..."
	ti.Focus()

	return BreakdownModel{
		run:       run,
		phase:     PhaseAnalyzing,
		spinner:   NewSpinner(),
		breakdown: breakdown.NewBreakdown(run.FeatureDesc, run.WorktreePath),
		textInput: ti,
	}
}

// Init initializes the breakdown model
func (m BreakdownModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startAnalysis(),
	)
}

// Update handles messages
func (m BreakdownModel) Update(msg tea.Msg) (BreakdownModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case analysisCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if len(msg.questions) > 0 {
			m.questions = msg.questions
			m.phase = PhaseQuestions
		} else {
			m.phase = PhaseBreakingDown
			return m, m.startBreakdown()
		}
		return m, nil

	case breakdownCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.tasks = msg.tasks
		m.phase = PhaseReview
		return m, nil
	}

	return m, nil
}

func (m BreakdownModel) handleKeyMsg(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	switch m.phase {
	case PhaseQuestions:
		return m.handleQuestionInput(msg)
	case PhaseReview:
		return m.handleReviewInput(msg)
	}
	return m, nil
}

func (m BreakdownModel) handleQuestionInput(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	q := m.questions[m.currentQuestion]

	switch msg.String() {
	case "up", "k":
		if len(q.Options) > 0 && m.selectedOption > 0 {
			m.selectedOption--
		}
	case "down", "j":
		if len(q.Options) > 0 && m.selectedOption < len(q.Options)-1 {
			m.selectedOption++
		}
	case "enter":
		// Record answer
		if len(q.Options) > 0 {
			m.questions[m.currentQuestion].Answer = q.Options[m.selectedOption]
		} else {
			m.questions[m.currentQuestion].Answer = m.textInput.Value()
		}

		// Move to next question or start breakdown
		m.currentQuestion++
		m.selectedOption = 0
		m.textInput.Reset()

		if m.currentQuestion >= len(m.questions) {
			// Apply answers to breakdown
			m.breakdown.Questions = m.questions
			m.phase = PhaseBreakingDown
			return m, m.startBreakdown()
		}
	default:
		if len(q.Options) == 0 {
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m BreakdownModel) handleReviewInput(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selectedTask > 0 {
			m.selectedTask--
		}
	case "down", "j":
		if m.selectedTask < len(m.tasks)-1 {
			m.selectedTask++
		}
	case "enter", "y":
		// Confirm and proceed to execution
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenConfirm}
		}
	case "e":
		// Edit selected task (placeholder)
		// TODO: implement task editing
	}

	return m, nil
}

// View renders the breakdown screen
func (m BreakdownModel) View() string {
	var b strings.Builder

	// Header
	b.WriteString(titleStyle.Render("Feature Breakdown"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(TruncateString(m.run.FeatureDesc, 60)))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
		return b.String()
	}

	switch m.phase {
	case PhaseAnalyzing:
		b.WriteString(m.spinner.View())
		b.WriteString(" Analyzing codebase...")
	case PhaseQuestions:
		b.WriteString(m.viewQuestions())
	case PhaseBreakingDown:
		b.WriteString(m.spinner.View())
		b.WriteString(" Breaking down feature into tasks...")
	case PhaseReview:
		b.WriteString(m.viewReview())
	}

	return b.String()
}

func (m BreakdownModel) viewQuestions() string {
	var b strings.Builder

	q := m.questions[m.currentQuestion]
	progress := fmt.Sprintf("Question %d of %d", m.currentQuestion+1, len(m.questions))
	b.WriteString(dimStyle.Render(progress))
	b.WriteString("\n\n")

	b.WriteString(infoStyle.Render("? "))
	b.WriteString(q.Question)
	b.WriteString("\n\n")

	if len(q.Options) > 0 {
		for i, opt := range q.Options {
			if i == m.selectedOption {
				b.WriteString(selectedStyle.Render("> " + opt))
			} else {
				b.WriteString(normalStyle.Render("  " + opt))
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString(m.textInput.View())
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Press Enter to continue"))

	return b.String()
}

func (m BreakdownModel) viewReview() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Generated %d tasks:\n\n", len(m.tasks)))

	for i, task := range m.tasks {
		prefix := "  "
		style := normalStyle
		if i == m.selectedTask {
			prefix = "> "
			style = selectedStyle
		}

		line := fmt.Sprintf("%s%d. %s", prefix, i+1, task.Title)
		b.WriteString(style.Render(line))
		b.WriteString("\n")

		if i == m.selectedTask {
			// Show details for selected task
			b.WriteString(dimStyle.Render(fmt.Sprintf("     Priority: %d", task.Priority)))
			b.WriteString("\n")
			if len(task.DependsOn) > 0 {
				b.WriteString(dimStyle.Render(fmt.Sprintf("     Depends on: %v", task.DependsOn)))
				b.WriteString("\n")
			}
			if len(task.FilesWrite)+len(task.FilesCreate) > 0 {
				files := append(task.FilesWrite, task.FilesCreate...)
				b.WriteString(dimStyle.Render(fmt.Sprintf("     Files: %v", files)))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓: navigate  Enter: confirm  e: edit"))

	return b.String()
}

// Commands

type analysisCompleteMsg struct {
	questions []breakdown.ClarificationQuestion
	err       error
}

type breakdownCompleteMsg struct {
	tasks []*state.Task
	err   error
}

func (m BreakdownModel) startAnalysis() tea.Cmd {
	return func() tea.Msg {
		// In a real implementation, this would call Claude Code
		// For now, return empty questions to skip to breakdown
		return analysisCompleteMsg{questions: nil, err: nil}
	}
}

func (m BreakdownModel) startBreakdown() tea.Cmd {
	return func() tea.Msg {
		// In a real implementation, this would call Claude Code
		// For now, return placeholder tasks
		tasks := []*state.Task{
			{
				ID:          "task-1",
				Title:       "Example Task 1",
				Description: "This is a placeholder task",
				Priority:    1,
				Status:      state.TaskStatusPending,
			},
			{
				ID:          "task-2",
				Title:       "Example Task 2",
				Description: "Another placeholder task",
				Priority:    2,
				DependsOn:   []string{"task-1"},
				Status:      state.TaskStatusPending,
			},
		}
		return breakdownCompleteMsg{tasks: tasks, err: nil}
	}
}
