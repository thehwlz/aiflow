package scheduler

import (
	"sort"

	"github.com/howell-aikit/aiflow/internal/context"
	"github.com/howell-aikit/aiflow/internal/state"
)

// Scheduler manages task execution order and parallelization
type Scheduler struct {
	run         *state.Run
	maxParallel int
}

// NewScheduler creates a new scheduler
func NewScheduler(run *state.Run, maxParallel int) *Scheduler {
	return &Scheduler{
		run:         run,
		maxParallel: maxParallel,
	}
}

// DependencyGraph represents task dependencies
type DependencyGraph struct {
	// Adjacency list: task ID -> tasks that depend on it
	dependents map[string][]string
	// Reverse: task ID -> tasks it depends on
	dependencies map[string][]string
	// In-degree count for each task
	inDegree map[string]int
}

// BuildDependencyGraph constructs the dependency graph from tasks
func (s *Scheduler) BuildDependencyGraph() *DependencyGraph {
	g := &DependencyGraph{
		dependents:   make(map[string][]string),
		dependencies: make(map[string][]string),
		inDegree:     make(map[string]int),
	}

	// Initialize
	for _, t := range s.run.Tasks {
		g.inDegree[t.ID] = 0
		g.dependents[t.ID] = nil
		g.dependencies[t.ID] = nil
	}

	// Build explicit dependencies
	for _, t := range s.run.Tasks {
		for _, depID := range t.DependsOn {
			g.dependents[depID] = append(g.dependents[depID], t.ID)
			g.dependencies[t.ID] = append(g.dependencies[t.ID], depID)
			g.inDegree[t.ID]++
		}
	}

	// Add implicit dependencies from file overlap
	for i, t1 := range s.run.Tasks {
		for j, t2 := range s.run.Tasks {
			if i >= j {
				continue
			}

			// Skip if already have explicit dependency
			hasExplicit := false
			for _, dep := range t2.DependsOn {
				if dep == t1.ID {
					hasExplicit = true
					break
				}
			}
			for _, dep := range t1.DependsOn {
				if dep == t2.ID {
					hasExplicit = true
					break
				}
			}
			if hasExplicit {
				continue
			}

			// Check file overlap
			if context.DetectFileOverlap(t1, t2) {
				// Lower priority task depends on higher priority
				if t1.Priority <= t2.Priority {
					// t2 depends on t1
					if !contains(g.dependencies[t2.ID], t1.ID) {
						g.dependents[t1.ID] = append(g.dependents[t1.ID], t2.ID)
						g.dependencies[t2.ID] = append(g.dependencies[t2.ID], t1.ID)
						g.inDegree[t2.ID]++
					}
				} else {
					// t1 depends on t2
					if !contains(g.dependencies[t1.ID], t2.ID) {
						g.dependents[t2.ID] = append(g.dependents[t2.ID], t1.ID)
						g.dependencies[t1.ID] = append(g.dependencies[t1.ID], t2.ID)
						g.inDegree[t1.ID]++
					}
				}
			}
		}
	}

	return g
}

// GenerateBatches creates execution batches respecting dependencies
func (s *Scheduler) GenerateBatches() [][]*state.Task {
	graph := s.BuildDependencyGraph()

	// Track completed tasks
	completed := s.run.GetCompletedTasks()

	// Adjust in-degrees based on completed tasks
	inDegree := make(map[string]int)
	for id, deg := range graph.inDegree {
		if completed[id] {
			continue
		}
		inDegree[id] = deg
		// Subtract completed dependencies
		for _, depID := range graph.dependencies[id] {
			if completed[depID] {
				inDegree[id]--
			}
		}
	}

	var batches [][]*state.Task
	remaining := len(s.run.Tasks) - len(completed)

	for remaining > 0 {
		// Find all tasks with in-degree 0 (no pending dependencies)
		var ready []*state.Task
		for _, t := range s.run.Tasks {
			if completed[t.ID] || t.Status == state.TaskStatusCompleted {
				continue
			}
			if inDegree[t.ID] == 0 {
				ready = append(ready, t)
			}
		}

		if len(ready) == 0 {
			// Cycle detected or all tasks done
			break
		}

		// Sort by priority
		sort.Slice(ready, func(i, j int) bool {
			return ready[i].Priority < ready[j].Priority
		})

		// Limit batch size
		if len(ready) > s.maxParallel {
			ready = ready[:s.maxParallel]
		}

		// Check file overlap within batch to ensure parallel safety
		batch := s.filterParallelSafe(ready)

		batches = append(batches, batch)

		// Mark batch tasks as "completed" for next iteration
		for _, t := range batch {
			completed[t.ID] = true
			remaining--

			// Update in-degrees of dependents
			for _, depID := range graph.dependents[t.ID] {
				if _, ok := inDegree[depID]; ok {
					inDegree[depID]--
				}
			}
		}
	}

	return batches
}

// filterParallelSafe removes tasks that would conflict if run in parallel
func (s *Scheduler) filterParallelSafe(tasks []*state.Task) []*state.Task {
	if len(tasks) <= 1 {
		return tasks
	}

	var safe []*state.Task
	safe = append(safe, tasks[0])

	for i := 1; i < len(tasks); i++ {
		candidate := tasks[i]
		canAdd := true

		for _, existing := range safe {
			if context.DetectFileOverlap(existing, candidate) {
				canAdd = false
				break
			}
		}

		if canAdd {
			safe = append(safe, candidate)
		}

		if len(safe) >= s.maxParallel {
			break
		}
	}

	return safe
}

// GetNextBatch returns the next batch of tasks ready for execution
func (s *Scheduler) GetNextBatch() []*state.Task {
	batches := s.GenerateBatches()
	if len(batches) == 0 {
		return nil
	}
	return batches[0]
}

// CanRunParallel checks if two tasks can run in parallel
func CanRunParallel(t1, t2 *state.Task) bool {
	// Check explicit dependencies
	for _, dep := range t1.DependsOn {
		if dep == t2.ID {
			return false
		}
	}
	for _, dep := range t2.DependsOn {
		if dep == t1.ID {
			return false
		}
	}

	// Check file overlap
	if context.DetectFileOverlap(t1, t2) {
		return false
	}

	return true
}

// TopologicalSort returns tasks in topological order
func (s *Scheduler) TopologicalSort() []*state.Task {
	batches := s.GenerateBatches()
	var sorted []*state.Task
	for _, batch := range batches {
		sorted = append(sorted, batch...)
	}
	return sorted
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
