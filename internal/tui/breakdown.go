package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/howell-aikit/aiflow/internal/breakdown"
	"github.com/howell-aikit/aiflow/internal/claude"
	"github.com/howell-aikit/aiflow/internal/config"
	"github.com/howell-aikit/aiflow/internal/state"
)

var debugLog *log.Logger

func init() {
	f, err := os.OpenFile("/tmp/aiflow-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		debugLog = log.New(f, "", log.LstdFlags)
	}
}

func logDebug(format string, args ...interface{}) {
	if debugLog != nil {
		debugLog.Printf(format, args...)
	}
}

// BreakdownPhase represents the current phase of breakdown
type BreakdownPhase int

const (
	PhaseInput    BreakdownPhase = iota // Ask what to build
	PhaseDetecting                       // Show project type detection
	PhasePlanning                        // Claude exploring and asking questions
	PhaseQuestion                        // Displaying a question from Claude
	PhaseReview                          // Review generated tasks
)

// BreakdownModel handles the breakdown screen
type BreakdownModel struct {
	cfg     *config.Config
	run     *state.Run
	store   *state.Store
	phase   BreakdownPhase
	spinner spinner.Model

	// Feature input (PhaseInput)
	featureInput textinput.Model

	// Project detection
	projectType string // "empty" or "existing"

	// Planning state (PhasePlanning/PhaseQuestion)
	streamClient    *claude.StreamingClient
	claudeOutput    []string       // Text output from Claude (for display)
	currentQuestion *questionState // Current question being displayed
	cancelPlanning  context.CancelFunc
	msgChan         chan tea.Msg // Channel for receiving messages from planning goroutine

	// Task review (PhaseReview)
	tasks        []*state.Task
	selectedTask int

	// State
	err error
}

// questionState holds the state of a question being displayed
type questionState struct {
	toolUseID       string
	questions       []claude.Question
	currentIdx      int               // Current question index
	answers         map[string]string // Collected answers
	selectedOption  int               // Currently selected option
	isOtherSelected bool
	otherInput      textarea.Model
}

// NewBreakdownModel creates a new breakdown model
func NewBreakdownModel(cfg *config.Config, run *state.Run, store *state.Store) BreakdownModel {
	featureInput := textinput.New()
	featureInput.Placeholder = "Describe what you want to build..."
	featureInput.Focus()
	featureInput.Width = 60

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
		cfg:          cfg,
		run:          run,
		store:        store,
		phase:        startPhase,
		spinner:      NewSpinner(),
		featureInput: featureInput,
		projectType:  projectType,
		claudeOutput: []string{},
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
		m.phase = PhasePlanning
		return m, m.startPlanning()

	case planningTextMsg:
		m.claudeOutput = append(m.claudeOutput, msg.text)
		// Keep only last 10 lines
		if len(m.claudeOutput) > 10 {
			m.claudeOutput = m.claudeOutput[len(m.claudeOutput)-10:]
		}
		return m, nil

	case planningQuestionMsg:
		logDebug("planningQuestionMsg: entering PhaseQuestion with %d questions", len(msg.questions))
		m.phase = PhaseQuestion
		otherInput := textarea.New()
		otherInput.Placeholder = "Type your answer (Enter for new line, Ctrl+D to submit)..."
		otherInput.SetWidth(60)
		otherInput.SetHeight(4)
		otherInput.ShowLineNumbers = false
		m.currentQuestion = &questionState{
			toolUseID:  msg.toolUseID,
			questions:  msg.questions,
			currentIdx: 0,
			answers:    make(map[string]string),
			otherInput: otherInput,
		}
		return m, nil

	case planningCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.tasks = msg.tasks
		m.phase = PhaseReview
		return m, nil

	case planningErrorMsg:
		logDebug("planningErrorMsg: %v", msg.err)
		m.err = msg.err
		return m, nil

	case pollChannelMsg:
		// Poll for more messages from planning goroutine
		if m.msgChan == nil {
			logDebug("pollChannelMsg: msgChan is nil")
			return m, nil
		}
		select {
		case msg, ok := <-m.msgChan:
			if !ok {
				// Channel closed, planning complete
				logDebug("pollChannelMsg: channel closed, phase=%d", m.phase)
				m.msgChan = nil
				return m, nil
			}
			logDebug("pollChannelMsg: received message type=%T", msg)
			// Process the message and continue polling
			newModel, cmd := m.Update(msg)
			return newModel, tea.Batch(cmd, newModel.pollChannel())
		default:
			// No message available, schedule next poll with a small delay
			return m, m.pollChannelDelayed()
		}
	}

	return m, nil
}

// pollChannel returns a command that polls the message channel immediately
func (m BreakdownModel) pollChannel() tea.Cmd {
	return func() tea.Msg {
		return pollChannelMsg{}
	}
}

// pollChannelDelayed returns a command that polls after a small delay
func (m BreakdownModel) pollChannelDelayed() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return pollChannelMsg{}
	})
}

func (m BreakdownModel) handleKeyMsg(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	switch m.phase {
	case PhaseInput:
		return m.handleFeatureInput(msg)
	case PhasePlanning:
		// Allow cancellation
		if msg.String() == "esc" {
			if m.cancelPlanning != nil {
				m.cancelPlanning()
			}
			return m, tea.Quit
		}
		return m, nil
	case PhaseQuestion:
		return m.handleQuestionInput(msg)
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

func (m BreakdownModel) handleQuestionInput(msg tea.KeyMsg) (BreakdownModel, tea.Cmd) {
	if m.currentQuestion == nil {
		return m, nil
	}

	q := m.currentQuestion
	currentQ := q.questions[q.currentIdx]
	totalOptions := len(currentQ.Options) + 1 // +1 for "Other"
	otherIdx := totalOptions - 1

	// If typing in "Other" input
	if q.isOtherSelected {
		switch msg.String() {
		case "ctrl+d":
			answer := strings.TrimSpace(q.otherInput.Value())
			if answer == "" {
				return m, nil
			}
			return m.submitQuestionAnswer(answer)
		case "esc":
			q.isOtherSelected = false
			q.otherInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			q.otherInput, cmd = q.otherInput.Update(msg)
			return m, cmd
		}
	}

	// Option selection mode
	switch msg.String() {
	case "up", "k":
		if q.selectedOption > 0 {
			q.selectedOption--
		}
	case "down", "j":
		if q.selectedOption < totalOptions-1 {
			q.selectedOption++
		}
	case "enter":
		if q.selectedOption == otherIdx {
			q.isOtherSelected = true
			q.otherInput.Reset()
			q.otherInput.Focus()
			return m, textarea.Blink
		}
		return m.submitQuestionAnswer(currentQ.Options[q.selectedOption].Label)
	case "esc":
		if m.cancelPlanning != nil {
			m.cancelPlanning()
		}
		return m, tea.Quit
	}

	return m, nil
}

func (m BreakdownModel) submitQuestionAnswer(answer string) (BreakdownModel, tea.Cmd) {
	q := m.currentQuestion
	currentQ := q.questions[q.currentIdx]

	// Store the answer using the header as key
	key := currentQ.Header
	if key == "" {
		key = fmt.Sprintf("q%d", q.currentIdx)
	}
	q.answers[key] = answer

	// Move to next question or submit all answers
	q.currentIdx++
	q.selectedOption = 0
	q.isOtherSelected = false
	q.otherInput.Reset()
	q.otherInput.Blur()

	if q.currentIdx >= len(q.questions) {
		// All questions answered, send tool result back to Claude
		m.phase = PhasePlanning
		return m, m.sendQuestionAnswers()
	}

	return m, nil
}

func (m BreakdownModel) sendQuestionAnswers() tea.Cmd {
	return func() tea.Msg {
		if m.currentQuestion == nil || m.streamClient == nil {
			return planningErrorMsg{err: fmt.Errorf("no pending question")}
		}

		result := claude.AnswerAskUserQuestion(m.currentQuestion.toolUseID, m.currentQuestion.answers)
		if err := m.streamClient.SendToolResult(result); err != nil {
			return planningErrorMsg{err: fmt.Errorf("failed to send answer: %w", err)}
		}

		m.currentQuestion = nil
		return nil
	}
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
		// Save tasks to run before transitioning
		m.run.Tasks = m.tasks
		m.run.Status = state.RunStatusReady
		if m.store != nil {
			m.store.SaveRun(m.run)
		}
		return m, func() tea.Msg {
			return ScreenTransitionMsg{Screen: ScreenConfirm}
		}
	case "esc", "q":
		return m, tea.Quit
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
	case PhasePlanning:
		b.WriteString(m.viewPlanning())
	case PhaseQuestion:
		b.WriteString(m.viewQuestion())
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

func (m BreakdownModel) viewPlanning() string {
	var b strings.Builder

	// Project context
	projectLabel := "Existing project"
	if m.projectType == "empty" {
		projectLabel = "New project"
	}
	b.WriteString(dimStyle.Render(projectLabel))
	b.WriteString("\n\n")

	b.WriteString(m.spinner.View())
	b.WriteString(" Claude is exploring the codebase...")
	b.WriteString("\n\n")

	// Show recent output
	if len(m.claudeOutput) > 0 {
		b.WriteString(dimStyle.Render("Recent activity:"))
		b.WriteString("\n")
		for _, line := range m.claudeOutput {
			truncated := TruncateString(line, 70)
			b.WriteString(dimStyle.Render("  " + truncated))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Press Esc to cancel"))

	return b.String()
}

func (m BreakdownModel) viewQuestion() string {
	var b strings.Builder

	if m.currentQuestion == nil {
		return b.String()
	}

	q := m.currentQuestion
	if q.currentIdx >= len(q.questions) {
		return b.String()
	}

	currentQ := q.questions[q.currentIdx]

	// Progress indicator
	if len(q.questions) > 1 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("Question %d of %d", q.currentIdx+1, len(q.questions))))
		b.WriteString("\n\n")
	}

	// Question
	b.WriteString(infoStyle.Render("? "))
	b.WriteString(currentQ.Question)
	b.WriteString("\n\n")

	// Options or text input
	if q.isOtherSelected {
		b.WriteString("  " + q.otherInput.View())
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("  Ctrl+D to submit • Esc to go back"))
	} else {
		for i, opt := range currentQ.Options {
			isSelected := i == q.selectedOption

			var optLine strings.Builder
			if isSelected {
				optLine.WriteString(selectedStyle.Render("  > "))
			} else {
				optLine.WriteString("    ")
			}

			label := opt.Label
			if isSelected {
				optLine.WriteString(selectedStyle.Render(label))
			} else {
				optLine.WriteString(normalStyle.Render(label))
			}

			b.WriteString(optLine.String())
			b.WriteString("\n")

			if opt.Description != "" && isSelected {
				b.WriteString(dimStyle.Render(fmt.Sprintf("      %s", opt.Description)))
				b.WriteString("\n")
			}
		}

		// "Other" option
		otherIdx := len(currentQ.Options)
		if q.selectedOption == otherIdx {
			b.WriteString(selectedStyle.Render("  > Other"))
		} else {
			b.WriteString(normalStyle.Render("    Other"))
		}
		b.WriteString("\n")

		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  ↑/↓ select • Enter to confirm"))
	}

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
		b.WriteString(successStyle.Render(fmt.Sprintf("Parallel groups: %s", strings.Join(groupInfo, ", "))))
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

		titleLine := fmt.Sprintf("%s%d. %s", prefix, i+1, task.Title)
		if task.ParallelGroup != "" {
			titleLine += fmt.Sprintf(" [%s]", task.ParallelGroup)
		}
		b.WriteString(style.Render(titleLine))
		b.WriteString("\n")

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

type planningTextMsg struct {
	text string
}

type planningQuestionMsg struct {
	toolUseID string
	questions []claude.Question
}

type planningCompleteMsg struct {
	tasks []*state.Task
	err   error
}

type planningErrorMsg struct {
	err error
}

type pollChannelMsg struct{}

func (m BreakdownModel) showProjectType() tea.Cmd {
	return func() tea.Msg {
		return projectTypeMsg{projectType: m.run.ProjectType}
	}
}

func (m *BreakdownModel) startPlanning() tea.Cmd {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelPlanning = cancel

	// Get config values
	claudePath := ""
	if m.cfg != nil {
		claudePath = m.cfg.ClaudeCodePath
	}
	workDir := m.run.WorktreePath
	if workDir == "" {
		workDir = "."
	}

	// Create streaming client
	m.streamClient = claude.NewStreamingClient(claude.StreamingClientConfig{
		ClaudePath: claudePath,
		WorkDir:    workDir,
	})

	// Channel for receiving messages
	m.msgChan = make(chan tea.Msg, 100)

	// Start streaming in goroutine
	go func() {
		logDebug("startPlanning: goroutine started")
		defer func() {
			logDebug("startPlanning: goroutine ending, closing channel")
			close(m.msgChan)
		}()

		prompt := claude.BuildPlanningPrompt(m.run.FeatureDesc, m.projectType)

		err := m.streamClient.Start(ctx, prompt, claude.StreamOptions{
			SystemPrompt:    claude.PlanningSystemPrompt,
			SkipPermissions: true,

			OnText: func(text string) {
				// Check for breakdown JSON in the text
				if strings.Contains(text, `"type": "breakdown"`) || strings.Contains(text, `"type":"breakdown"`) {
					tasks, err := parseBreakdownFromText(text)
					if err == nil && len(tasks) > 0 {
						m.msgChan <- planningCompleteMsg{tasks: tasks}
						m.streamClient.Stop()
						return
					}
				}
				m.msgChan <- planningTextMsg{text: TruncateString(strings.TrimSpace(text), 100)}
			},

			OnToolUse: func(toolUse *claude.ToolUse) (*claude.ToolResult, error) {
				if toolUse.IsAskUserQuestion() {
					input, err := toolUse.ParseAskUserQuestionInput()
					if err != nil {
						return nil, err
					}
					// Send question to TUI and wait for answer
					// The answer will be sent via SendToolResult
					m.msgChan <- planningQuestionMsg{
						toolUseID: toolUse.ID,
						questions: input.Questions,
					}
					// Return nil to indicate we're handling this asynchronously
					return nil, nil
				}
				// Let Claude handle other tools
				return nil, nil
			},

			OnError: func(err error) {
				m.msgChan <- planningErrorMsg{err: err}
			},
		})

		if err != nil {
			m.msgChan <- planningErrorMsg{err: err}
			return
		}

		// Wait for completion
		if err := m.streamClient.Wait(); err != nil {
			// Don't report error if we intentionally stopped
			if ctx.Err() == nil {
				m.msgChan <- planningErrorMsg{err: err}
			}
		}
	}()

	// Start polling the channel
	return m.pollChannel()
}

// parseBreakdownFromText extracts and parses a breakdown JSON from text
func parseBreakdownFromText(text string) ([]*state.Task, error) {
	// Find JSON object in text
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON found")
	}

	jsonStr := text[start : end+1]

	// Parse the breakdown
	var result struct {
		Type    string `json:"type"`
		Summary string `json:"summary"`
		Tasks   []breakdown.TaskSpec `json:"tasks"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, err
	}

	if result.Type != "breakdown" {
		return nil, fmt.Errorf("not a breakdown response")
	}

	return breakdown.ConvertToTasks(result.Tasks), nil
}
