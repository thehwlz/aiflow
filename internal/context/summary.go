package context

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/howell-aikit/aiflow/internal/state"
)

// SummaryExtractionPrompt is the prompt used to extract summaries from completed tasks
const SummaryExtractionPrompt = `Analyze the changes you just made and extract a structured summary in JSON format:

{
  "files_changed": ["list of modified files"],
  "files_created": ["list of new files"],
  "functions_added": ["function signatures, e.g., 'NewUser(email string) *User'"],
  "types_added": ["type definitions, e.g., 'User struct', 'AuthToken struct'"],
  "patterns_used": ["architectural patterns, e.g., 'Repository pattern', 'Middleware chain'"],
  "decisions": ["key design decisions with brief rationale"],
  "conventions": ["coding conventions followed, e.g., 'Errors wrapped with fmt.Errorf'"],
  "gotchas": ["things future tasks should know about"],
  "public_interface": "brief description of main exports and how to use them"
}

Respond ONLY with the JSON object, no additional text.`

// ParseSummary parses a JSON summary response from Claude
func ParseSummary(taskID, response string) (*state.TaskSummary, error) {
	// Find JSON in response (may have extra text)
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[start : end+1]

	var raw struct {
		FilesChanged    []string `json:"files_changed"`
		FilesCreated    []string `json:"files_created"`
		FunctionsAdded  []string `json:"functions_added"`
		TypesAdded      []string `json:"types_added"`
		PatternsUsed    []string `json:"patterns_used"`
		Decisions       []string `json:"decisions"`
		Conventions     []string `json:"conventions"`
		Gotchas         []string `json:"gotchas"`
		PublicInterface string   `json:"public_interface"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse summary JSON: %w", err)
	}

	return &state.TaskSummary{
		TaskID:          taskID,
		FilesChanged:    raw.FilesChanged,
		FilesCreated:    raw.FilesCreated,
		FunctionsAdded:  raw.FunctionsAdded,
		TypesAdded:      raw.TypesAdded,
		PatternsUsed:    raw.PatternsUsed,
		Decisions:       raw.Decisions,
		Conventions:     raw.Conventions,
		Gotchas:         raw.Gotchas,
		PublicInterface: raw.PublicInterface,
	}, nil
}

// FormatFullSummary formats a complete summary for direct dependencies
func FormatFullSummary(summary *state.TaskSummary, taskTitle string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## Summary from Task: %s\n\n", taskTitle))

	if len(summary.FilesChanged) > 0 {
		b.WriteString("**Files Changed:** ")
		b.WriteString(strings.Join(summary.FilesChanged, ", "))
		b.WriteString("\n\n")
	}

	if len(summary.FilesCreated) > 0 {
		b.WriteString("**Files Created:** ")
		b.WriteString(strings.Join(summary.FilesCreated, ", "))
		b.WriteString("\n\n")
	}

	if len(summary.FunctionsAdded) > 0 {
		b.WriteString("**Functions Added:**\n")
		for _, f := range summary.FunctionsAdded {
			b.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		b.WriteString("\n")
	}

	if len(summary.TypesAdded) > 0 {
		b.WriteString("**Types Added:**\n")
		for _, t := range summary.TypesAdded {
			b.WriteString(fmt.Sprintf("- `%s`\n", t))
		}
		b.WriteString("\n")
	}

	if len(summary.PatternsUsed) > 0 {
		b.WriteString("**Patterns Used:** ")
		b.WriteString(strings.Join(summary.PatternsUsed, ", "))
		b.WriteString("\n\n")
	}

	if len(summary.Decisions) > 0 {
		b.WriteString("**Key Decisions:**\n")
		for _, d := range summary.Decisions {
			b.WriteString(fmt.Sprintf("- %s\n", d))
		}
		b.WriteString("\n")
	}

	if len(summary.Conventions) > 0 {
		b.WriteString("**Conventions:**\n")
		for _, c := range summary.Conventions {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
		b.WriteString("\n")
	}

	if len(summary.Gotchas) > 0 {
		b.WriteString("**Important Notes:**\n")
		for _, g := range summary.Gotchas {
			b.WriteString(fmt.Sprintf("- %s\n", g))
		}
		b.WriteString("\n")
	}

	if summary.PublicInterface != "" {
		b.WriteString("**Public Interface:** ")
		b.WriteString(summary.PublicInterface)
		b.WriteString("\n")
	}

	return b.String()
}

// FormatLightSummary formats a condensed summary for non-dependent tasks in same feature
func FormatLightSummary(summary *state.TaskSummary, taskTitle string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## Context from Task: %s\n\n", taskTitle))

	// Only include files and key decisions
	allFiles := append(summary.FilesChanged, summary.FilesCreated...)
	if len(allFiles) > 0 {
		b.WriteString("**Files Modified:** ")
		b.WriteString(strings.Join(allFiles, ", "))
		b.WriteString("\n\n")
	}

	if len(summary.Decisions) > 0 {
		b.WriteString("**Decisions:**\n")
		for _, d := range summary.Decisions {
			b.WriteString(fmt.Sprintf("- %s\n", d))
		}
		b.WriteString("\n")
	}

	if summary.PublicInterface != "" {
		b.WriteString("**Exports:** ")
		b.WriteString(summary.PublicInterface)
		b.WriteString("\n")
	}

	return b.String()
}

// SummaryTokens estimates token count for a summary
func SummaryTokens(summary *state.TaskSummary, full bool) int {
	var formatted string
	if full {
		formatted = FormatFullSummary(summary, "")
	} else {
		formatted = FormatLightSummary(summary, "")
	}
	return EstimateTokens(formatted)
}
