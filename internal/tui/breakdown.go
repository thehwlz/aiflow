package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/howell-aikit/aiflow/internal/breakdown"
	"github.com/howell-aikit/aiflow/internal/state"
)

// BreakdownPhase represents the current phase of breakdown
type BreakdownPhase int

const (
	PhaseInput        BreakdownPhase = iota // Ask what to build
	PhaseDetecting                          // Show project type detection
	PhaseConversation                       // Adaptive Q&A conversation
	PhaseGenerating                         // Generating breakdown
	PhaseReview                             // Review generated tasks
)

// BreakdownModel handles the breakdown screen
type BreakdownModel struct {
	run     *state.Run
	phase   BreakdownPhase
	spinner spinner.Model

	// Feature input (PhaseInput)
	featureInput textinput.Model

	// Project detection
	projectType string // "empty" or "existing"

	// Conversation handling (PhaseConversation)
	conversation    *breakdown.SpecConversation
	currentQuestion *breakdown.ConversationQuestion
	selectedOption  int             // Currently selected option index
	isOtherSelected bool            // True when "Other" is selected
	otherInput      textinput.Model // Input for custom answer
	safetyLimit     int             // Max questions as safety valve

	// Task review (PhaseReview)
	tasks        []*state.Task
	selectedTask int

	// State
	err error
}

// NewBreakdownModel creates a new breakdown model
func NewBreakdownModel(run *state.Run) BreakdownModel {
	featureInput := textinput.New()
	featureInput.Placeholder = "Describe what you want to build..."
	featureInput.Focus()
	featureInput.Width = 60

	otherInput := textinput.New()
	otherInput.Placeholder = "Type your answer..."
	otherInput.Width = 60

	// Determine starting phase
	startPhase := PhaseInput
	if run.FeatureDesc != "" {
		startPhase = PhaseDetecting
	}

	// Get project type from run if already set
	projectType := run.ProjectType
	if projectType == "" {
		projectType = "existing"
	}

	return BreakdownModel{
		run:          run,
		phase:        startPhase,
		spinner:      NewSpinner(),
		featureInput: featureInput,
		otherInput:   otherInput,
		projectType:  projectType,
		safetyLimit:  10,
	}
}

// Init initializes the breakdown model
func (m BreakdownModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}

	switch m.phase {
	case PhaseInput:
		cmds = append(cmds, textinput.Blink)
	case PhaseDetecting:
		cmds = append(cmds, m.showProjectType())
	}

	return tea.Batch(cmds...)
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

	case projectTypeMsg:
		m.projectType = msg.projectType
		m.phase = PhaseConversation
		m.conversation = breakdown.NewSpecConversation(
			m.run.FeatureDesc,
			breakdown.ProjectType(m.projectType),
			m.safetyLimit,
		)
		return m, m.fetchNextQuestion()

	case specResponseMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m.handleSpecResponse(msg.response)

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
	case PhaseInput:
		return m.handleFeatureInput(msg)
	case PhaseConversation:
		return m.handleConversationInput(msg)
	case PhaseReview:
		return m.handleReviewInput(msg)
	}
	return m, nil
}

func (m BreakdownModel) handleFeatureInput(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := strings.TrimSpace(m.featureInput.Value())
		if value == "" {
			return m, nil
		}
		m.run.FeatureDesc = value
		m.phase = PhaseDetecting
		return m, m.showProjectType()
	case "esc":
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.featureInput, cmd = m.featureInput.Update(msg)
		return m, cmd
	}
}

func (m BreakdownModel) handleConversationInput(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	if m.currentQuestion == nil {
		return m, nil
	}

	totalOptions := len(m.currentQuestion.Options) + 1 // +1 for "Other"
	otherIdx := totalOptions - 1

	// If typing in "Other" input
	if m.isOtherSelected {
		switch msg.String() {
		case "enter":
			answer := strings.TrimSpace(m.otherInput.Value())
			if answer == "" {
				return m, nil
			}
			return m.submitAnswer(answer)
		case "esc":
			m.isOtherSelected = false
			return m, nil
		default:
			var cmd tea.Cmd
			m.otherInput, cmd = m.otherInput.Update(msg)
			return m, cmd
		}
	}

	// Option selection mode
	switch msg.String() {
	case "up", "k":
		if m.selectedOption > 0 {
			m.selectedOption--
		}
	case "down", "j":
		if m.selectedOption < totalOptions-1 {
			m.selectedOption++
		}
	case "enter":
		if m.selectedOption == otherIdx {
			m.isOtherSelected = true
			m.otherInput.Reset()
			m.otherInput.Focus()
			return m, textinput.Blink
		}
		return m.submitAnswer(m.currentQuestion.Options[m.selectedOption].Label)
	case "s":
		m.phase = PhaseGenerating
		return m, m.forceBreakdown()
	case "esc":
		return m, tea.Quit
	}

	return m, nil
}

func (m BreakdownModel) submitAnswer(answer string) (BreakdownModel, tea.Cmd) {
	m.conversation.AddTurn(*m.currentQuestion, answer)

	if m.run.SpecConversation == nil {
		m.run.SpecConversation = &state.SpecConversation{
			Turns:    []state.SpecTurn{},
			MaxTurns: m.safetyLimit,
		}
	}

	// Convert options for storage
	var stateOpts []state.SpecQuestionOption
	for _, opt := range m.currentQuestion.Options {
		stateOpts = append(stateOpts, state.SpecQuestionOption{
			Label:       opt.Label,
			Description: opt.Description,
			Recommended: opt.Recommended,
		})
	}

	m.run.SpecConversation.Turns = append(m.run.SpecConversation.Turns, state.SpecTurn{
		Question: state.SpecQuestion{
			ID:      m.currentQuestion.ID,
			Text:    m.currentQuestion.Text,
			Options: stateOpts,
		},
		Answer: answer,
	})

	m.currentQuestion = nil
	m.selectedOption = 0
	m.isOtherSelected = false
	m.otherInput.Reset()

	return m, m.fetchNextQuestion()
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
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenConfirm}
		}
	case "esc", "q":
		return m, tea.Quit
	}

	return m, nil
}

func (m BreakdownModel) handleSpecResponse(resp *breakdown.SpecResponse) (BreakdownModel, tea.Cmd) {
	switch resp.ResponseType {
	case "question":
		if m.conversation != nil && !m.conversation.CanAskMore() {
			m.phase = PhaseGenerating
			return m, m.forceBreakdown()
		}
		m.currentQuestion = resp.Question
		m.selectedOption = 0
		m.isOtherSelected = false
		return m, nil

	case "ready":
		m.phase = PhaseGenerating
		tasks := breakdown.ConvertToTasks(resp.Breakdown.Tasks)
		return m, func() tea.Msg {
			return breakdownCompleteMsg{tasks: tasks, err: nil}
		}
	}

	return m, nil
}

// View renders the breakdown screen
func (m BreakdownModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("aiflow"))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("Press Esc to quit"))
		return b.String()
	}

	switch m.phase {
	case PhaseInput:
		b.WriteString(m.viewInput())
	case PhaseDetecting:
		b.WriteString(m.viewDetecting())
	case PhaseConversation:
		b.WriteString(m.viewConversation())
	case PhaseGenerating:
		b.WriteString(m.viewGenerating())
	case PhaseReview:
		b.WriteString(m.viewReview())
	}

	return b.String()
}

func (m BreakdownModel) viewInput() string {
	var b strings.Builder

	b.WriteString(infoStyle.Render("? "))
	b.WriteString("What would you like to build?\n\n")
	b.WriteString("  " + m.featureInput.View())
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Enter to continue"))

	return b.String()
}

func (m BreakdownModel) viewDetecting() string {
	var b strings.Builder

	b.WriteString(m.spinner.View())
	b.WriteString(" Analyzing project...")

	return b.String()
}

func (m BreakdownModel) viewConversation() string {
	var b strings.Builder

	// Project context
	projectLabel := "Existing project"
	if m.projectType == "empty" {
		projectLabel = "New project"
	}
	b.WriteString(dimStyle.Render(projectLabel))
	b.WriteString("\n\n")

	// Conversation history
	if m.conversation != nil && len(m.conversation.Turns) > 0 {
		for _, turn := range m.conversation.Turns {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %s → %s",
				TruncateString(turn.Question.Text, 40),
				turn.Answer)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if m.currentQuestion == nil {
		b.WriteString(m.spinner.View())
		b.WriteString(" Thinking...")
		return b.String()
	}

	// Question
	b.WriteString(infoStyle.Render("? "))
	b.WriteString(m.currentQuestion.Text)
	b.WriteString("\n\n")

	// Options or text input
	if m.isOtherSelected {
		b.WriteString("  " + m.otherInput.View())
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("  Enter to submit • Esc to go back"))
	} else {
		// Show options with recommendations and descriptions
		for i, opt := range m.currentQuestion.Options {
			isSelected := i == m.selectedOption

			// Build option display
			var optLine strings.Builder
			if isSelected {
				optLine.WriteString(selectedStyle.Render("  > "))
			} else {
				optLine.WriteString("    ")
			}

			// Label with recommendation badge
			label := opt.Label
			if opt.Recommended {
				label += " (Recommended)"
			}

			if isSelected {
				optLine.WriteString(selectedStyle.Render(label))
			} else {
				optLine.WriteString(normalStyle.Render(label))
			}

			b.WriteString(optLine.String())
			b.WriteString("\n")

			// Show description for selected option or all recommendations
			if opt.Description != "" && (isSelected || opt.Recommended) {
				b.WriteString(dimStyle.Render(fmt.Sprintf("      %s", opt.Description)))
				b.WriteString("\n")
			}
		}

		// "Other" option
		otherIdx := len(m.currentQuestion.Options)
		if m.selectedOption == otherIdx {
			b.WriteString(selectedStyle.Render("  > Other"))
		} else {
			b.WriteString(normalStyle.Render("    Other"))
		}
		b.WriteString("\n")

		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  ↑/↓ select • Enter to confirm • s to skip"))
	}

	return b.String()
}

func (m BreakdownModel) viewGenerating() string {
	var b strings.Builder

	b.WriteString(m.spinner.View())
	b.WriteString(" Generating task breakdown...")

	return b.String()
}

func (m BreakdownModel) viewReview() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render(TruncateString(m.run.FeatureDesc, 60)))
	b.WriteString("\n\n")

	// Count parallel groups
	parallelGroups := make(map[string]int)
	for _, task := range m.tasks {
		if task.ParallelGroup != "" {
			parallelGroups[task.ParallelGroup]++
		}
	}

	// Show parallelization info
	if len(parallelGroups) > 0 {
		var groupInfo []string
		for group, count := range parallelGroups {
			groupInfo = append(groupInfo, fmt.Sprintf("%s (%d)", group, count))
		}
		b.WriteString(successStyle.Render(fmt.Sprintf("⚡ Parallel groups: %s", strings.Join(groupInfo, ", "))))
		b.WriteString("\n\n")
	}

	b.WriteString(fmt.Sprintf("Generated %d tasks:\n\n", len(m.tasks)))

	for i, task := range m.tasks {
		prefix := "    "
		style := normalStyle
		if i == m.selectedTask {
			prefix = "  > "
			style = selectedStyle
		}

		// Task title with parallel indicator
		titleLine := fmt.Sprintf("%s%d. %s", prefix, i+1, task.Title)
		if task.ParallelGroup != "" {
			titleLine += fmt.Sprintf(" [%s]", task.ParallelGroup)
		}
		b.WriteString(style.Render(titleLine))
		b.WriteString("\n")

		// Show details for selected task
		if i == m.selectedTask {
			if task.Description != "" {
				wrapped := wrapText(task.Description, 50)
				for _, line := range wrapped {
					b.WriteString(dimStyle.Render("      " + line))
					b.WriteString("\n")
				}
			}
			if len(task.DependsOn) > 0 {
				b.WriteString(dimStyle.Render(fmt.Sprintf("      Waits for: %v", task.DependsOn)))
				b.WriteString("\n")
			}
			if task.ParallelGroup != "" {
				// Find other tasks in same group
				var siblings []string
				for _, other := range m.tasks {
					if other.ID != task.ID && other.ParallelGroup == task.ParallelGroup {
						siblings = append(siblings, other.Title)
					}
				}
				if len(siblings) > 0 {
					b.WriteString(successStyle.Render(fmt.Sprintf("      ⚡ Runs parallel with: %s", strings.Join(siblings, ", "))))
					b.WriteString("\n")
				}
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ↑/↓ navigate • Enter to confirm • Esc to cancel"))

	return b.String()
}

func wrapText(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}
	var lines []string
	words := strings.Fields(text)
	var current string
	for _, word := range words {
		if len(current)+len(word)+1 > width {
			if current != "" {
				lines = append(lines, current)
			}
			current = word
		} else {
			if current == "" {
				current = word
			} else {
				current += " " + word
			}
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// Messages

type projectTypeMsg struct {
	projectType string
}

type specResponseMsg struct {
	response *breakdown.SpecResponse
	err      error
}

type breakdownCompleteMsg struct {
	tasks []*state.Task
	err   error
}

func (m BreakdownModel) showProjectType() tea.Cmd {
	return func() tea.Msg {
		return projectTypeMsg{projectType: m.run.ProjectType}
	}
}

func (m BreakdownModel) fetchNextQuestion() tea.Cmd {
	return func() tea.Msg {
		turn := 0
		if m.conversation != nil {
			turn = len(m.conversation.Turns)
		}

		// Simulated adaptive responses
		if m.projectType == "empty" {
			switch turn {
			case 0:
				return specResponseMsg{
					response: &breakdown.SpecResponse{
						ResponseType: "question",
						Question: &breakdown.ConversationQuestion{
							ID:   "q1",
							Text: "What language/framework would you like to use?",
							Options: []breakdown.QuestionOption{
								{
									Label:       "Go",
									Description: "Best for CLIs, APIs, and system tools",
									Recommended: true,
								},
								{
									Label:       "TypeScript/Node.js",
									Description: "Best for web apps and full-stack projects",
								},
								{
									Label:       "Python",
									Description: "Best for scripts, data, and ML projects",
								},
								{
									Label:       "Rust",
									Description: "Best for performance-critical applications",
								},
							},
						},
					},
				}
			case 1:
				return specResponseMsg{
					response: &breakdown.SpecResponse{
						ResponseType: "question",
						Question: &breakdown.ConversationQuestion{
							ID:   "q2",
							Text: "What type of application is this?",
							Options: []breakdown.QuestionOption{
								{
									Label:       "CLI tool",
									Description: "Command-line interface application",
								},
								{
									Label:       "REST API",
									Description: "Backend HTTP API service",
									Recommended: true,
								},
								{
									Label:       "Web application",
									Description: "Full-stack web app with frontend",
								},
								{
									Label:       "Library/package",
									Description: "Reusable code for other projects",
								},
							},
						},
					},
				}
			}
		}

		return m.simulateBreakdown()
	}
}

func (m BreakdownModel) simulateBreakdown() tea.Msg {
	tasks := []*state.Task{
		{
			ID:            "task-1",
			Title:         "Create project configuration",
			Description:   "Set up go.mod, config files, and project structure",
			Priority:      1,
			ParallelGroup: "setup",
			Status:        state.TaskStatusPending,
		},
		{
			ID:            "task-2",
			Title:         "Define core types and interfaces",
			Description:   "Create the main data structures and interfaces",
			Priority:      1,
			ParallelGroup: "setup",
			Status:        state.TaskStatusPending,
		},
		{
			ID:            "task-3",
			Title:         "Set up dependencies",
			Description:   "Install and configure required packages",
			Priority:      1,
			ParallelGroup: "setup",
			Status:        state.TaskStatusPending,
		},
		{
			ID:            "task-4",
			Title:         "Implement main logic",
			Description:   "Build the core feature functionality",
			Priority:      2,
			DependsOn:     []string{"task-1", "task-2", "task-3"},
			Status:        state.TaskStatusPending,
		},
		{
			ID:            "task-5",
			Title:         "Add unit tests",
			Description:   "Write tests for core functionality",
			Priority:      3,
			ParallelGroup: "testing",
			DependsOn:     []string{"task-4"},
			Status:        state.TaskStatusPending,
		},
		{
			ID:            "task-6",
			Title:         "Add integration tests",
			Description:   "Write end-to-end tests",
			Priority:      3,
			ParallelGroup: "testing",
			DependsOn:     []string{"task-4"},
			Status:        state.TaskStatusPending,
		},
	}
	return breakdownCompleteMsg{tasks: tasks, err: nil}
}

func (m BreakdownModel) forceBreakdown() tea.Cmd {
	return func() tea.Msg {
		return m.simulateBreakdown()
	}
}
