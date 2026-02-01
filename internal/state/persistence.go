package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Store handles persistence of run state
type Store struct {
	stateDir string
}

// NewStore creates a new state store
func NewStore(stateDir string) (*Store, error) {
	runsDir := filepath.Join(stateDir, "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}
	return &Store{stateDir: stateDir}, nil
}

// CreateRun creates a new run and persists it
func (s *Store) CreateRun(featureDesc, worktreePath, baseBranch string) (*Run, error) {
	run := &Run{
		ID:           uuid.New().String()[:8],
		FeatureDesc:  featureDesc,
		WorktreePath: worktreePath,
		BaseBranch:   baseBranch,
		Tasks:        []*Task{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Status:       RunStatusBreakdown,
	}

	if err := s.SaveRun(run); err != nil {
		return nil, err
	}

	if err := s.SetCurrentRun(run.ID); err != nil {
		return nil, err
	}

	return run, nil
}

// SaveRun persists a run to disk
func (s *Store) SaveRun(run *Run) error {
	run.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run: %w", err)
	}

	runPath := s.runPath(run.ID)
	if err := os.WriteFile(runPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write run file: %w", err)
	}

	return nil
}

// LoadRun loads a run from disk
func (s *Store) LoadRun(id string) (*Run, error) {
	runPath := s.runPath(id)
	data, err := os.ReadFile(runPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("run %s not found", id)
		}
		return nil, fmt.Errorf("failed to read run file: %w", err)
	}

	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("failed to unmarshal run: %w", err)
	}

	return &run, nil
}

// DeleteRun removes a run from disk
func (s *Store) DeleteRun(id string) error {
	runPath := s.runPath(id)
	if err := os.Remove(runPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete run file: %w", err)
	}

	// Clear current if this was the current run
	currentID, _ := s.GetCurrentRunID()
	if currentID == id {
		_ = s.ClearCurrentRun()
	}

	return nil
}

// ListRuns returns all runs sorted by creation time (newest first)
func (s *Store) ListRuns() ([]*Run, error) {
	runsDir := filepath.Join(s.stateDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read runs directory: %w", err)
	}

	var runs []*Run
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := entry.Name()[:len(entry.Name())-5] // Remove .json
		run, err := s.LoadRun(id)
		if err != nil {
			continue // Skip corrupted files
		}
		runs = append(runs, run)
	}

	// Sort by creation time, newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})

	return runs, nil
}

// SetCurrentRun sets the current active run
func (s *Store) SetCurrentRun(id string) error {
	currentPath := filepath.Join(s.stateDir, "current.json")
	data, err := json.Marshal(map[string]string{"run_id": id})
	if err != nil {
		return err
	}
	return os.WriteFile(currentPath, data, 0644)
}

// GetCurrentRunID returns the ID of the current run
func (s *Store) GetCurrentRunID() (string, error) {
	currentPath := filepath.Join(s.stateDir, "current.json")
	data, err := os.ReadFile(currentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var current map[string]string
	if err := json.Unmarshal(data, &current); err != nil {
		return "", err
	}

	return current["run_id"], nil
}

// GetCurrentRun returns the current active run
func (s *Store) GetCurrentRun() (*Run, error) {
	id, err := s.GetCurrentRunID()
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	return s.LoadRun(id)
}

// ClearCurrentRun clears the current run pointer
func (s *Store) ClearCurrentRun() error {
	currentPath := filepath.Join(s.stateDir, "current.json")
	return os.Remove(currentPath)
}

// runPath returns the file path for a run
func (s *Store) runPath(id string) string {
	return filepath.Join(s.stateDir, "runs", id+".json")
}

// AddTask adds a task to a run
func (s *Store) AddTask(runID string, task *Task) error {
	run, err := s.LoadRun(runID)
	if err != nil {
		return err
	}

	run.Tasks = append(run.Tasks, task)
	return s.SaveRun(run)
}

// UpdateTask updates a task within a run
func (s *Store) UpdateTask(runID, taskID string, updateFn func(*Task)) error {
	run, err := s.LoadRun(runID)
	if err != nil {
		return err
	}

	task := run.GetTask(taskID)
	if task == nil {
		return fmt.Errorf("task %s not found in run %s", taskID, runID)
	}

	updateFn(task)
	return s.SaveRun(run)
}

// SetTaskStatus updates a task's status
func (s *Store) SetTaskStatus(runID, taskID string, status TaskStatus) error {
	return s.UpdateTask(runID, taskID, func(t *Task) {
		t.Status = status
		now := time.Now()
		switch status {
		case TaskStatusRunning:
			t.StartedAt = &now
		case TaskStatusCompleted, TaskStatusFailed:
			t.CompletedAt = &now
		}
	})
}

// SetTaskSummary sets the summary for a completed task
func (s *Store) SetTaskSummary(runID, taskID string, summary *TaskSummary) error {
	return s.UpdateTask(runID, taskID, func(t *Task) {
		t.Summary = summary
	})
}

// SetTaskError sets an error message for a failed task
func (s *Store) SetTaskError(runID, taskID, errMsg string) error {
	return s.UpdateTask(runID, taskID, func(t *Task) {
		t.Error = errMsg
		t.Status = TaskStatusFailed
		now := time.Now()
		t.CompletedAt = &now
	})
}
