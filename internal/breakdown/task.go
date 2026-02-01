package breakdown

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/howell-aikit/aiflow/internal/state"
)

// TaskSpec represents a task specification from breakdown
type TaskSpec struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	FilesRead     []string `json:"files_read"`
	FilesWrite    []string `json:"files_write"`
	FilesCreate   []string `json:"files_create"`
	DependsOn     []string `json:"depends_on"`      // References by title or index
	Priority      int      `json:"priority"`
	ParallelGroup string   `json:"parallel_group"`  // Tasks in same group can run in parallel
}

// BreakdownResult contains the parsed breakdown from Claude
type BreakdownResult struct {
	Summary      string     `json:"summary"`
	Tasks        []TaskSpec `json:"tasks"`
	Questions    []string   `json:"questions,omitempty"`
	Assumptions  []string   `json:"assumptions,omitempty"`
}

// ParseBreakdownResponse parses Claude's breakdown response
func ParseBreakdownResponse(response string) (*BreakdownResult, error) {
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	jsonStr := response[start : end+1]

	var result BreakdownResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse breakdown JSON: %w", err)
	}

	return &result, nil
}

// ConvertToTasks converts TaskSpecs to state.Tasks
func ConvertToTasks(specs []TaskSpec) []*state.Task {
	// First pass: create tasks with temporary IDs
	tasks := make([]*state.Task, len(specs))
	titleToID := make(map[string]string)

	for i, spec := range specs {
		id := uuid.New().String()[:8]
		tasks[i] = &state.Task{
			ID:            id,
			Title:         spec.Title,
			Description:   spec.Description,
			FilesRead:     spec.FilesRead,
			FilesWrite:    spec.FilesWrite,
			FilesCreate:   spec.FilesCreate,
			Priority:      spec.Priority,
			ParallelGroup: spec.ParallelGroup,
			Status:        state.TaskStatusPending,
		}
		titleToID[spec.Title] = id
		titleToID[fmt.Sprintf("%d", i)] = id // Also map by index
		titleToID[fmt.Sprintf("task%d", i)] = id
		titleToID[fmt.Sprintf("task_%d", i)] = id
	}

	// Second pass: resolve dependencies
	for i, spec := range specs {
		for _, dep := range spec.DependsOn {
			// Try to find by title or index
			depID, ok := titleToID[dep]
			if !ok {
				// Try case-insensitive match
				depLower := strings.ToLower(dep)
				for title, id := range titleToID {
					if strings.ToLower(title) == depLower {
						depID = id
						ok = true
						break
					}
				}
			}
			if ok {
				tasks[i].DependsOn = append(tasks[i].DependsOn, depID)
			}
		}
	}

	return tasks
}

// ValidateTasks checks task specifications for issues
func ValidateTasks(tasks []*state.Task) []string {
	var issues []string

	// Check for circular dependencies
	if hasCycle(tasks) {
		issues = append(issues, "Circular dependency detected in tasks")
	}

	// Check for missing dependencies
	taskIDs := make(map[string]bool)
	for _, t := range tasks {
		taskIDs[t.ID] = true
	}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if !taskIDs[dep] {
				issues = append(issues, fmt.Sprintf("Task %s depends on unknown task %s", t.ID, dep))
			}
		}
	}

	// Check for empty tasks
	for _, t := range tasks {
		if t.Title == "" {
			issues = append(issues, fmt.Sprintf("Task %s has empty title", t.ID))
		}
		if t.Description == "" {
			issues = append(issues, fmt.Sprintf("Task %s has empty description", t.ID))
		}
	}

	return issues
}

// hasCycle detects cycles in task dependencies using DFS
func hasCycle(tasks []*state.Task) bool {
	taskMap := make(map[string]*state.Task)
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(id string) bool
	dfs = func(id string) bool {
		visited[id] = true
		recStack[id] = true

		task, ok := taskMap[id]
		if !ok {
			return false
		}

		for _, dep := range task.DependsOn {
			if !visited[dep] {
				if dfs(dep) {
					return true
				}
			} else if recStack[dep] {
				return true
			}
		}

		recStack[id] = false
		return false
	}

	for _, t := range tasks {
		if !visited[t.ID] {
			if dfs(t.ID) {
				return true
			}
		}
	}

	return false
}

// EstimateTaskCount provides a rough estimate of tasks for a feature
func EstimateTaskCount(featureDesc string) int {
	// Simple heuristic based on description length and keywords
	words := len(strings.Fields(featureDesc))

	// Base estimate
	estimate := 3

	// Adjust for complexity indicators
	lower := strings.ToLower(featureDesc)
	if strings.Contains(lower, "full") || strings.Contains(lower, "complete") {
		estimate += 2
	}
	if strings.Contains(lower, "authentication") || strings.Contains(lower, "auth") {
		estimate += 2
	}
	if strings.Contains(lower, "api") {
		estimate += 1
	}
	if strings.Contains(lower, "test") || strings.Contains(lower, "testing") {
		estimate += 1
	}
	if strings.Contains(lower, "refactor") {
		estimate += 2
	}

	// Adjust for length
	if words > 20 {
		estimate += 2
	} else if words > 10 {
		estimate += 1
	}

	// Cap at reasonable range
	if estimate < 2 {
		estimate = 2
	}
	if estimate > 15 {
		estimate = 15
	}

	return estimate
}
