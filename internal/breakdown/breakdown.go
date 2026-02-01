package breakdown

// This package provides task breakdown utilities for aiflow.
//
// The main breakdown flow is now handled by Claude Code streaming in
// internal/tui/breakdown.go using the streaming client. This package
// provides the task conversion and validation utilities.
//
// See:
// - task.go: TaskSpec, ConvertToTasks, ValidateTasks
// - internal/claude/prompts.go: PlanningSystemPrompt
