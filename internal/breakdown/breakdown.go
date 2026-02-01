package breakdown

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BreakdownPromptTemplate is the template for feature breakdown
const BreakdownPromptTemplate = `You are analyzing a codebase to break down a feature into implementation tasks.

# Feature Request
%s

# Instructions

Analyze the codebase and break down this feature into discrete, implementable tasks. Each task should:
1. Be independently executable
2. Have clear file dependencies
3. Build upon previous tasks where needed

Respond with a JSON object in this exact format:
{
  "summary": "Brief summary of the implementation approach",
  "tasks": [
    {
      "title": "Short task title",
      "description": "Detailed implementation instructions",
      "files_read": ["existing files to reference"],
      "files_write": ["existing files to modify"],
      "files_create": ["new files to create"],
      "depends_on": ["titles of tasks this depends on"],
      "priority": 1
    }
  ],
  "assumptions": ["any assumptions made about requirements"],
  "questions": ["any clarifying questions (if critical)"]
}

Guidelines:
- Keep tasks focused and atomic
- Include setup tasks (dependencies, config) first
- Include test tasks where appropriate
- Use realistic file paths based on the codebase structure
- Priority should be 1-10, lower = execute first
- depends_on should reference task titles exactly

Analyze the codebase structure first, then provide the breakdown.`

// QuestionPromptTemplate prompts for clarifying questions
const QuestionPromptTemplate = `Based on your analysis, do you need any clarifications before breaking down this feature?

Feature: %s

If you have clarifying questions, respond with a JSON object:
{
  "questions": [
    {
      "question": "The question text",
      "options": ["option1", "option2"],
      "default": "suggested default if user skips"
    }
  ]
}

If no questions needed, respond with: {"questions": []}

Only ask questions that are critical for implementation. Prefer making reasonable assumptions.`

// ClarificationQuestion represents a question for the user
type ClarificationQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"`
	Answer   string   `json:"-"` // Filled in after user responds
}

// ProjectType indicates whether this is a new or existing project
type ProjectType string

const (
	ProjectTypeEmpty    ProjectType = "empty"
	ProjectTypeExisting ProjectType = "existing"
)

// QuestionOption represents a single option with optional description
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"` // When to pick this option
	Recommended bool   `json:"recommended,omitempty"` // Mark as recommended
}

// ConversationQuestion represents a single question in the adaptive conversation
type ConversationQuestion struct {
	ID      string           `json:"id"`
	Text    string           `json:"question"`
	Options []QuestionOption `json:"options,omitempty"`
	Default string           `json:"default,omitempty"`
}

// ConversationTurn represents one Q&A exchange
type ConversationTurn struct {
	Question ConversationQuestion
	Answer   string
}

// SpecConversation tracks the adaptive specification conversation
type SpecConversation struct {
	FeatureDesc string             `json:"feature_desc"`
	ProjectType ProjectType        `json:"project_type"`
	Turns       []ConversationTurn `json:"turns"`
	MaxTurns    int                `json:"max_turns"`
}

// SpecResponse represents Claude's response in the adaptive flow
type SpecResponse struct {
	ResponseType string                `json:"type"` // "question" | "ready"
	Question     *ConversationQuestion `json:"question,omitempty"`
	Breakdown    *BreakdownResult      `json:"breakdown,omitempty"`
}

// AdaptiveSpecPromptTemplate is the prompt for the adaptive conversation
const AdaptiveSpecPromptTemplate = `You are helping specify a feature before breaking it down into implementation tasks.

# Context
Project Type: %s
Feature Request: %s

# Conversation So Far
%s

# Instructions

Based on the conversation, decide what to do next:

**If you need clarification**, ask a single focused question:
{
  "type": "question",
  "question": {
    "id": "q%d",
    "question": "Your question here",
    "options": [
      {"label": "Option text", "description": "When to pick this", "recommended": true},
      {"label": "Another option", "description": "When to pick this"}
    ]
  }
}

**If you have enough information**, provide the task breakdown:
{
  "type": "ready",
  "breakdown": {
    "summary": "Brief implementation approach",
    "tasks": [
      {
        "title": "Task title",
        "description": "What to implement",
        "files_read": [],
        "files_write": [],
        "files_create": [],
        "depends_on": [],
        "priority": 1,
        "parallel_group": "group-name"
      }
    ],
    "assumptions": []
  }
}

## Task Design for Parallel Execution

IMPORTANT: Design tasks to maximize parallel execution using Claude Code subagents.

1. **Identify independent work streams** - Tasks that don't share file dependencies can run in parallel
2. **Group parallel tasks** - Assign the same "parallel_group" to tasks that can run simultaneously
3. **Minimize dependencies** - Only add depends_on when truly necessary (shared state, file conflicts)

Example parallel groups:
- "setup": Multiple config files can be created in parallel
- "core-features": Independent features can be built simultaneously
- "tests": Test files for different modules can be written in parallel

Good parallel breakdown:
- Task A (parallel_group: "setup") - Create config.go
- Task B (parallel_group: "setup") - Create types.go
- Task C (parallel_group: "setup") - Set up dependencies
- Task D (depends_on: [A,B,C]) - Implement main logic

Bad sequential breakdown:
- Task A - Create config.go
- Task B (depends_on: A) - Create types.go  <- unnecessary dependency!
- Task C (depends_on: B) - Set up dependencies <- unnecessary dependency!

Guidelines:
- Ask only what's necessary - many features can be broken down immediately
- For new projects: you may need to ask about tech stack or structure
- For existing projects: focus on how to integrate with existing code
- Be concise - one question at a time
- When in doubt, make reasonable assumptions and note them
- MAXIMIZE parallelization - this is key for fast execution`

// Breakdown orchestrates the feature breakdown process
type Breakdown struct {
	FeatureDesc string
	WorkDir     string
	Questions   []ClarificationQuestion
	Result      *BreakdownResult
}

// NewBreakdown creates a new breakdown session
func NewBreakdown(featureDesc, workDir string) *Breakdown {
	return &Breakdown{
		FeatureDesc: featureDesc,
		WorkDir:     workDir,
	}
}

// GetBreakdownPrompt returns the prompt for initial breakdown
func (b *Breakdown) GetBreakdownPrompt() string {
	return fmt.Sprintf(BreakdownPromptTemplate, b.FeatureDesc)
}

// GetBreakdownPromptWithAnswers includes user answers to questions
func (b *Breakdown) GetBreakdownPromptWithAnswers() string {
	if len(b.Questions) == 0 {
		return b.GetBreakdownPrompt()
	}

	var answers strings.Builder
	answers.WriteString("\n\n# Clarifications\n")
	for _, q := range b.Questions {
		answer := q.Answer
		if answer == "" {
			answer = q.Default
		}
		answers.WriteString(fmt.Sprintf("- %s: %s\n", q.Question, answer))
	}

	return fmt.Sprintf(BreakdownPromptTemplate, b.FeatureDesc+answers.String())
}

// GetQuestionPrompt returns the prompt for clarifying questions
func (b *Breakdown) GetQuestionPrompt() string {
	return fmt.Sprintf(QuestionPromptTemplate, b.FeatureDesc)
}

// ParseQuestionsResponse parses the questions response
func (b *Breakdown) ParseQuestionsResponse(response string) error {
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		// No questions
		b.Questions = nil
		return nil
	}

	jsonStr := response[start : end+1]

	type questionsResult struct {
		Questions []ClarificationQuestion `json:"questions"`
	}

	var result questionsResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// If parsing fails, assume no questions
		b.Questions = nil
		return nil
	}

	b.Questions = result.Questions
	return nil
}

// SetResult sets the breakdown result
func (b *Breakdown) SetResult(result *BreakdownResult) {
	b.Result = result
}

// BreakdownSummary provides a text summary of the breakdown
func (b *Breakdown) BreakdownSummary() string {
	if b.Result == nil {
		return "No breakdown available"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Feature: %s\n", b.FeatureDesc))
	sb.WriteString(fmt.Sprintf("Summary: %s\n", b.Result.Summary))
	sb.WriteString(fmt.Sprintf("Tasks: %d\n\n", len(b.Result.Tasks)))

	for i, task := range b.Result.Tasks {
		sb.WriteString(fmt.Sprintf("%d. %s (priority: %d)\n", i+1, task.Title, task.Priority))
		if len(task.DependsOn) > 0 {
			sb.WriteString(fmt.Sprintf("   Depends on: %s\n", strings.Join(task.DependsOn, ", ")))
		}
		if len(task.FilesWrite)+len(task.FilesCreate) > 0 {
			files := append(task.FilesWrite, task.FilesCreate...)
			sb.WriteString(fmt.Sprintf("   Files: %s\n", strings.Join(files, ", ")))
		}
	}

	if len(b.Result.Assumptions) > 0 {
		sb.WriteString("\nAssumptions:\n")
		for _, a := range b.Result.Assumptions {
			sb.WriteString(fmt.Sprintf("- %s\n", a))
		}
	}

	return sb.String()
}

// NewSpecConversation creates a new adaptive specification conversation
func NewSpecConversation(featureDesc string, projectType ProjectType, maxTurns int) *SpecConversation {
	if maxTurns <= 0 {
		maxTurns = 8
	}
	return &SpecConversation{
		FeatureDesc: featureDesc,
		ProjectType: projectType,
		Turns:       []ConversationTurn{},
		MaxTurns:    maxTurns,
	}
}

// AddTurn adds a completed Q&A turn to the conversation
func (c *SpecConversation) AddTurn(question ConversationQuestion, answer string) {
	c.Turns = append(c.Turns, ConversationTurn{
		Question: question,
		Answer:   answer,
	})
}

// CurrentTurn returns the current turn number (1-indexed)
func (c *SpecConversation) CurrentTurn() int {
	return len(c.Turns) + 1
}

// CanAskMore returns true if more questions are allowed
func (c *SpecConversation) CanAskMore() bool {
	return len(c.Turns) < c.MaxTurns
}

// GetConversationPrompt builds the prompt with full conversation history
func (c *SpecConversation) GetConversationPrompt() string {
	// Build conversation history
	var history strings.Builder
	if len(c.Turns) == 0 {
		history.WriteString("(No questions asked yet)")
	} else {
		for _, turn := range c.Turns {
			history.WriteString(fmt.Sprintf("Q: %s\n", turn.Question.Text))
			history.WriteString(fmt.Sprintf("A: %s\n\n", turn.Answer))
		}
	}

	return fmt.Sprintf(AdaptiveSpecPromptTemplate,
		c.ProjectType,
		c.FeatureDesc,
		history.String(),
		c.CurrentTurn(),
	)
}

// GetForcedBreakdownPrompt returns a prompt that forces breakdown with available info
func (c *SpecConversation) GetForcedBreakdownPrompt() string {
	var history strings.Builder
	if len(c.Turns) == 0 {
		history.WriteString("(No clarifications collected)")
	} else {
		for i, turn := range c.Turns {
			history.WriteString(fmt.Sprintf("- %s: %s\n", turn.Question.Text, turn.Answer))
			_ = i // suppress unused warning
		}
	}

	return fmt.Sprintf(`You are a feature specification assistant. The user wants to proceed with the breakdown now.

# Context
Project Type: %s
Feature Request: %s

# Clarifications Collected
%s

# Instructions

Create a task breakdown based on the information available. Make reasonable assumptions where needed.

Respond with ONLY the breakdown JSON:
{
  "type": "ready",
  "breakdown": {
    "summary": "Brief summary of the implementation approach",
    "tasks": [
      {
        "title": "Task title",
        "description": "Detailed implementation instructions",
        "files_read": [],
        "files_write": [],
        "files_create": [],
        "depends_on": [],
        "priority": 1
      }
    ],
    "assumptions": ["assumptions made due to limited information"]
  }
}`,
		c.ProjectType,
		c.FeatureDesc,
		history.String(),
	)
}

// ParseSpecResponse parses Claude's response in the adaptive flow
func ParseSpecResponse(response string) (*SpecResponse, error) {
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[start : end+1]

	var result SpecResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse spec response JSON: %w", err)
	}

	// Validate response type
	if result.ResponseType != "question" && result.ResponseType != "ready" {
		return nil, fmt.Errorf("invalid response type: %s", result.ResponseType)
	}

	if result.ResponseType == "question" && result.Question == nil {
		return nil, fmt.Errorf("question response missing question field")
	}

	if result.ResponseType == "ready" && result.Breakdown == nil {
		return nil, fmt.Errorf("ready response missing breakdown field")
	}

	return &result, nil
}
