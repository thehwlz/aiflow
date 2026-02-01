package state

import (
	"time"
)

// TaskStatus represents the current state of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusReady     TaskStatus = "ready"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// TaskSummary contains structured knowledge extracted after task completion
type TaskSummary struct {
	TaskID          string   `json:"task_id"`
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

// Task represents a single unit of work within a feature
type Task struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Description   string       `json:"description"`
	FilesRead     []string     `json:"files_read"`
	FilesWrite    []string     `json:"files_write"`
	FilesCreate   []string     `json:"files_create"`
	DependsOn     []string     `json:"depends_on"`
	Priority      int          `json:"priority"`
	ParallelGroup string       `json:"parallel_group,omitempty"` // Tasks in same group can run in parallel
	Status        TaskStatus   `json:"status"`
	Summary       *TaskSummary `json:"summary,omitempty"`
	Error         string       `json:"error,omitempty"`
	StartedAt     *time.Time   `json:"started_at,omitempty"`
	CompletedAt   *time.Time   `json:"completed_at,omitempty"`
}

// IsReady returns true if the task can be executed
func (t *Task) IsReady(completedTasks map[string]bool) bool {
	if t.Status != TaskStatusPending {
		return false
	}
	for _, dep := range t.DependsOn {
		if !completedTasks[dep] {
			return false
		}
	}
	return true
}

// SpecQuestionOption represents an option for a spec question
type SpecQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

// SpecQuestion represents a question asked during spec conversation
type SpecQuestion struct {
	ID      string               `json:"id"`
	Text    string               `json:"question"`
	Options []SpecQuestionOption `json:"options,omitempty"`
	Default string               `json:"default,omitempty"`
}

// SpecTurn represents one Q&A exchange in the spec conversation
type SpecTurn struct {
	Question SpecQuestion `json:"question"`
	Answer   string       `json:"answer"`
}

// SpecConversation tracks the adaptive specification conversation
type SpecConversation struct {
	Turns    []SpecTurn `json:"turns"`
	MaxTurns int        `json:"max_turns"`
}

// Run represents a complete feature implementation run
type Run struct {
	ID               string            `json:"id"`
	FeatureDesc      string            `json:"feature_desc"`
	WorktreePath     string            `json:"worktree_path"`
	BaseBranch       string            `json:"base_branch"`
	Tasks            []*Task           `json:"tasks"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	Status           RunStatus         `json:"status"`
	Error            string            `json:"error,omitempty"`
	ProjectType      string            `json:"project_type,omitempty"`      // "empty" or "existing"
	SpecConversation *SpecConversation `json:"spec_conversation,omitempty"` // Adaptive spec Q&A
}

// RunStatus represents the overall status of a run
type RunStatus string

const (
	RunStatusBreakdown  RunStatus = "breakdown"
	RunStatusReady      RunStatus = "ready"
	RunStatusRunning    RunStatus = "running"
	RunStatusCompleted  RunStatus = "completed"
	RunStatusFailed     RunStatus = "failed"
	RunStatusCancelled  RunStatus = "cancelled"
)

// GetTask returns a task by ID
func (r *Run) GetTask(id string) *Task {
	for _, t := range r.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// GetCompletedTasks returns a map of completed task IDs
func (r *Run) GetCompletedTasks() map[string]bool {
	completed := make(map[string]bool)
	for _, t := range r.Tasks {
		if t.Status == TaskStatusCompleted {
			completed[t.ID] = true
		}
	}
	return completed
}

// GetReadyTasks returns tasks that are ready to execute
func (r *Run) GetReadyTasks() []*Task {
	completed := r.GetCompletedTasks()
	var ready []*Task
	for _, t := range r.Tasks {
		if t.IsReady(completed) {
			ready = append(ready, t)
		}
	}
	return ready
}

// GetRunningTasks returns currently running tasks
func (r *Run) GetRunningTasks() []*Task {
	var running []*Task
	for _, t := range r.Tasks {
		if t.Status == TaskStatusRunning {
			running = append(running, t)
		}
	}
	return running
}

// GetPendingTasks returns tasks that are pending
func (r *Run) GetPendingTasks() []*Task {
	var pending []*Task
	for _, t := range r.Tasks {
		if t.Status == TaskStatusPending {
			pending = append(pending, t)
		}
	}
	return pending
}

// GetFailedTasks returns tasks that failed
func (r *Run) GetFailedTasks() []*Task {
	var failed []*Task
	for _, t := range r.Tasks {
		if t.Status == TaskStatusFailed {
			failed = append(failed, t)
		}
	}
	return failed
}

// IsComplete returns true if all tasks are completed
func (r *Run) IsComplete() bool {
	for _, t := range r.Tasks {
		if t.Status != TaskStatusCompleted {
			return false
		}
	}
	return len(r.Tasks) > 0
}

// Progress returns the completion percentage
func (r *Run) Progress() float64 {
	if len(r.Tasks) == 0 {
		return 0
	}
	completed := 0
	for _, t := range r.Tasks {
		if t.Status == TaskStatusCompleted {
			completed++
		}
	}
	return float64(completed) / float64(len(r.Tasks)) * 100
}

// ResetRunningTasks resets running tasks back to pending (for resume)
func (r *Run) ResetRunningTasks() {
	for _, t := range r.Tasks {
		if t.Status == TaskStatusRunning {
			t.Status = TaskStatusPending
			t.StartedAt = nil
		}
	}
}
