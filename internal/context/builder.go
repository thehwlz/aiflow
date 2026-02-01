package context

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/howell-aikit/aiflow/internal/config"
	"github.com/howell-aikit/aiflow/internal/state"
)

// Builder constructs context for task execution
type Builder struct {
	workDir string
	cfg     *config.Config
	run     *state.Run
}

// NewBuilder creates a new context builder
func NewBuilder(workDir string, cfg *config.Config, run *state.Run) *Builder {
	return &Builder{
		workDir: workDir,
		cfg:     cfg,
		run:     run,
	}
}

// BuildContext constructs the full context for a task
func (b *Builder) BuildContext(task *state.Task) (string, error) {
	budget := NewTokenBudget(b.cfg.ContextMaxTokens, 500) // Reserve for prompt template

	var parts []string

	// 1. Task description
	taskPart := b.formatTaskDescription(task)
	parts = append(parts, taskPart)
	budget.Use(EstimateTokens(taskPart))

	// 2. Summaries from completed tasks (hybrid context)
	summaryPart, err := b.buildSummaryContext(task, budget)
	if err != nil {
		return "", err
	}
	if summaryPart != "" {
		parts = append(parts, summaryPart)
	}

	// 3. File contents for files_read
	filesPart, err := b.buildFilesContext(task.FilesRead, budget)
	if err != nil {
		return "", err
	}
	if filesPart != "" {
		parts = append(parts, filesPart)
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// formatTaskDescription formats the task for the prompt
func (b *Builder) formatTaskDescription(task *state.Task) string {
	var sb strings.Builder

	sb.WriteString("# Task\n\n")
	sb.WriteString(fmt.Sprintf("**%s**\n\n", task.Title))
	sb.WriteString(task.Description)
	sb.WriteString("\n")

	if len(task.FilesWrite) > 0 {
		sb.WriteString("\n**Files to modify:** ")
		sb.WriteString(strings.Join(task.FilesWrite, ", "))
		sb.WriteString("\n")
	}

	if len(task.FilesCreate) > 0 {
		sb.WriteString("\n**Files to create:** ")
		sb.WriteString(strings.Join(task.FilesCreate, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildSummaryContext builds context from completed task summaries
func (b *Builder) buildSummaryContext(task *state.Task, budget *TokenBudget) (string, error) {
	if !b.cfg.Summaries.IncludeForDependencies && !b.cfg.Summaries.IncludeForSameFeature {
		return "", nil
	}

	var parts []string
	directDeps := make(map[string]bool)
	for _, dep := range task.DependsOn {
		directDeps[dep] = true
	}

	// Collect summaries
	type summaryEntry struct {
		task     *state.Task
		isDirect bool
	}

	var entries []summaryEntry
	for _, t := range b.run.Tasks {
		if t.ID == task.ID || t.Status != state.TaskStatusCompleted || t.Summary == nil {
			continue
		}

		isDirect := directDeps[t.ID]
		if isDirect && b.cfg.Summaries.IncludeForDependencies {
			entries = append(entries, summaryEntry{task: t, isDirect: true})
		} else if !isDirect && b.cfg.Summaries.IncludeForSameFeature {
			entries = append(entries, summaryEntry{task: t, isDirect: false})
		}
	}

	// Sort: direct dependencies first, then by priority
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDirect != entries[j].isDirect {
			return entries[i].isDirect
		}
		return entries[i].task.Priority < entries[j].task.Priority
	})

	// Add summaries within budget
	maxPerSummary := b.cfg.Summaries.MaxSummaryTokens
	for _, entry := range entries {
		var formatted string
		if entry.isDirect {
			formatted = FormatFullSummary(entry.task.Summary, entry.task.Title)
		} else {
			formatted = FormatLightSummary(entry.task.Summary, entry.task.Title)
		}

		tokens := EstimateTokens(formatted)
		if tokens > maxPerSummary {
			formatted = TruncateToTokens(formatted, maxPerSummary)
			tokens = maxPerSummary
		}

		if budget.CanFit(tokens) {
			budget.Use(tokens)
			parts = append(parts, formatted)
		}
	}

	if len(parts) == 0 {
		return "", nil
	}

	return "# Context from Prior Tasks\n\n" + strings.Join(parts, "\n"), nil
}

// buildFilesContext reads and formats file contents
func (b *Builder) buildFilesContext(files []string, budget *TokenBudget) (string, error) {
	if len(files) == 0 {
		return "", nil
	}

	// Limit number of files
	maxFiles := b.cfg.ContextMaxFiles
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}

	var parts []string
	parts = append(parts, "# File Contents\n")

	for _, file := range files {
		fullPath := filepath.Join(b.workDir, file)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				// File doesn't exist yet, skip
				continue
			}
			return "", fmt.Errorf("failed to read %s: %w", file, err)
		}

		formatted := fmt.Sprintf("## %s\n\n```\n%s\n```", file, string(content))
		tokens := EstimateTokens(formatted)

		if budget.CanFit(tokens) {
			budget.Use(tokens)
			parts = append(parts, formatted)
		} else {
			// Try to fit truncated version
			minTokens := 100
			if truncated, ok := budget.TryFitContent(string(content), minTokens); ok {
				formatted = fmt.Sprintf("## %s (truncated)\n\n```\n%s\n```", file, truncated)
				parts = append(parts, formatted)
			}
		}
	}

	if len(parts) <= 1 {
		return "", nil
	}

	return strings.Join(parts, "\n\n"), nil
}

// BuildTaskPrompt constructs the full prompt for Claude Code
func (b *Builder) BuildTaskPrompt(task *state.Task) (string, error) {
	context, err := b.BuildContext(task)
	if err != nil {
		return "", err
	}

	prompt := fmt.Sprintf(`You are implementing a feature for a software project. Complete the following task.

%s

Guidelines:
- Focus only on this specific task
- Follow existing code patterns and conventions
- Write clean, maintainable code
- Test your changes if applicable
- Do not modify files outside the scope of this task

When complete, the changes will be reviewed before merging.`, context)

	return prompt, nil
}

// DetectFileOverlap checks if two tasks have overlapping file access
func DetectFileOverlap(t1, t2 *state.Task) bool {
	// Check if t1 writes to files t2 reads or writes
	t1Writes := make(map[string]bool)
	for _, f := range t1.FilesWrite {
		t1Writes[f] = true
	}
	for _, f := range t1.FilesCreate {
		t1Writes[f] = true
	}

	for _, f := range t2.FilesRead {
		if t1Writes[f] {
			return true
		}
	}
	for _, f := range t2.FilesWrite {
		if t1Writes[f] {
			return true
		}
	}

	// Check if t2 writes to files t1 reads or writes
	t2Writes := make(map[string]bool)
	for _, f := range t2.FilesWrite {
		t2Writes[f] = true
	}
	for _, f := range t2.FilesCreate {
		t2Writes[f] = true
	}

	for _, f := range t1.FilesRead {
		if t2Writes[f] {
			return true
		}
	}
	for _, f := range t1.FilesWrite {
		if t2Writes[f] {
			return true
		}
	}

	return false
}
