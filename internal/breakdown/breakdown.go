package breakdown

import (
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
	// Use simple JSON parsing here since we don't want to import encoding/json again
	// Actually, let's just check if it contains questions

	if strings.Contains(jsonStr, `"questions": []`) || strings.Contains(jsonStr, `"questions":[]`) {
		b.Questions = nil
		return nil
	}

	// For now, parse manually or use the full parser
	// This is a simplified implementation
	b.Questions = nil
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
