# aiflow - AI Agent Orchestrator for Feature Implementation

## Project Location

```
~/side-projects/aiflow/
```

## Overview

A Go-based CLI tool that orchestrates Claude Code to implement features through:
1. Isolated git worktrees per feature run
2. Interactive feature breakdown into tasks
3. **Hybrid context management** - fresh context + structured summaries from prior tasks
4. Parallel execution of independent tasks with file locking

## Tech Stack

- **Go 1.22+** - Single binary, no runtime needed
- **Cobra** - CLI framework
- **Bubble Tea** - Terminal UI for interactive prompts
- **go-git** - Git worktree management (no git CLI dependency)
- **gofrs/flock** - File locking for parallel safety
- **go-toml** - Configuration parsing

## Project Structure

```
~/side-projects/aiflow/
├── cmd/aiflow/main.go           # Entry point
├── internal/
│   ├── cli/                     # Cobra commands (start, status, list, clean, resume)
│   ├── config/                  # TOML config loading
│   ├── worktree/                # Git worktree management
│   ├── breakdown/               # Feature → tasks via Claude Code
│   ├── context/                 # Hybrid context builder
│   │   ├── builder.go           # Context assembly
│   │   ├── summary.go           # Task summary extraction
│   │   └── tokens.go            # Token estimation
│   ├── scheduler/               # Dependency graph + parallel batching
│   ├── executor/                # Claude Code invocation
│   ├── state/                   # Persistence + resume capability
│   └── tui/                     # Bubble Tea screens
├── pkg/git/                     # Git operations wrapper
└── configs/default.toml         # Default config template
```

## Core Workflow

```
User: aiflow start "Add user authentication"
         │
         ▼
┌─────────────────────────────────┐
│ 1. Create worktree from main    │
│    → .aiflow-worktrees/xxx/     │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 2. Interactive breakdown (TUI)  │
│    • Claude analyzes codebase   │
│    • Asks clarifying questions  │
│    • Generates task list        │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 3. Analyze dependencies         │
│    • File overlap detection     │
│    • Build execution batches    │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 4. Execute tasks                │
│    • Parallel batches           │
│    • File locking               │
│    • Hybrid context per task    │
│    • Extract summary on done    │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 5. Review in worktree           │
│    • User validates changes     │
│    • Merge when ready           │
└─────────────────────────────────┘
```

## Key Components

### 1. CLI Commands

| Command | Description |
|---------|-------------|
| `aiflow start <desc>` | Start new feature (creates worktree, runs breakdown) |
| `aiflow status [id]` | Show run status |
| `aiflow list` | List all worktrees |
| `aiflow clean [id]` | Remove worktree(s) |
| `aiflow resume [id]` | Resume interrupted run |

### 2. Task Structure

```go
type Task struct {
    ID          string   // Unique identifier
    Title       string   // Short description
    Description string   // Detailed instructions
    FilesRead   []string // Context files (for building prompt)
    FilesWrite  []string // Files to lock during execution
    FilesCreate []string // New files to lock
    DependsOn   []string // Explicit task dependencies
    Priority    int      // Execution order tiebreaker
    Status      string   // pending/ready/running/completed/failed
    Summary     *TaskSummary // Extracted after completion
}
```

### 3. Hybrid Context Management (Key Feature)

**Problem:** Full context carryover wastes tokens; fresh-only context loses cross-task awareness.

**Solution:** Fresh context + structured summaries from completed tasks.

#### Task Summary Structure

```go
type TaskSummary struct {
    TaskID        string
    FilesChanged  []string
    FilesCreated  []string

    // Structured knowledge extracted by Claude
    FunctionsAdded  []string  // "NewUser(email string) *User"
    TypesAdded      []string  // "User struct", "AuthToken struct"
    PatternsUsed    []string  // "Repository pattern", "Middleware chain"
    Decisions       []string  // "Used JWT over sessions because X"
    Conventions     []string  // "Errors wrapped with fmt.Errorf"
    Gotchas         []string  // "Email must be unique constraint"
    PublicInterface string    // Key exports for dependent tasks
}
```

#### Context Building Flow

```
Task 3 depends on Task 1 and Task 2

Task 3 receives:
├── Fresh files: [handlers/auth.go, routes.go]  ← Only what it needs
├── Task 1 summary (direct dependency)
│   └── Full structured summary
├── Task 2 summary (direct dependency)
│   └── Full structured summary
└── Task 0 summary (same feature, no dependency)
    └── Light summary (files + decisions only)
```

#### Tiered Detail Based on Relationship

| Relationship | Context Included |
|--------------|------------------|
| Direct dependency (`depends_on`) | Full structured summary |
| Same feature, no dependency | Light summary (files + key decisions) |
| File overlap detected | Relevant file excerpts |
| No relationship | Nothing |

#### Summary Extraction Prompt

After each task completes, Claude extracts the summary:

```
Analyze the changes you just made and extract a structured summary:

{
  "files_changed": ["list of modified files"],
  "files_created": ["list of new files"],
  "functions_added": ["function signatures"],
  "types_added": ["type definitions"],
  "patterns_used": ["architectural patterns"],
  "decisions": ["key design decisions with rationale"],
  "conventions": ["coding conventions followed"],
  "gotchas": ["things future tasks should know"],
  "public_interface": "brief description of exports"
}
```

#### Benefits

| Aspect | Full Carryover | Fresh Only | Hybrid |
|--------|----------------|------------|--------|
| Token usage | High (grows) | Low (constant) | Low (constant) |
| Cross-task awareness | High | None | Medium-High |
| Agent focus | Diluted | Sharp | Sharp |
| Decisions preserved | Yes | No | Yes |
| Patterns kept | Yes | No | Yes |

### 4. Parallel Execution Strategy

**Dependency Detection:**
- Explicit: `depends_on` field (task B needs task A's output)
- Implicit: File overlap (task B writes what task A reads)

**Batch Generation:**
```
Tasks: T1(writes: user.go), T2(writes: auth.go), T3(reads: user.go, writes: handler.go)

Analysis:
- T1 and T2: No overlap → parallel
- T3 depends on T1 (reads user.go) → sequential after T1

Batches: [[T1, T2], [T3]]
```

**File Locking:**
- Lock `files_write` + `files_create` before execution
- `.aiflow.lock` suffix for lock files
- Auto-release on completion/failure

### 5. State Management

```
~/.aiflow/state/
├── runs/
│   └── {run-id}.json    # Full run state with tasks + summaries
└── current.json         # Active run pointer
```

Resume capability:
- Interrupted runs continue from last completed task
- Summaries from completed tasks preserved
- Running tasks reset to "ready" on resume

## Implementation Order

### Phase 1: Foundation
1. **Project setup** (`~/side-projects/aiflow/`)
   - Initialize Go module
   - Set up directory structure

2. **CLI skeleton** (`internal/cli/`)
   - Root command with global flags
   - Config loading from `~/.aiflow/config.toml`

3. **Worktree manager** (`internal/worktree/`)
   - Create worktree from branch
   - List/remove/prune operations
   - Naming: `.aiflow-worktrees/{feature-slug}-{timestamp}/`

4. **State persistence** (`internal/state/`)
   - Run struct with tasks
   - JSON file save/load
   - Resume logic

### Phase 2: Core Logic
5. **Feature breakdown** (`internal/breakdown/`)
   - Claude Code prompts for analysis
   - Question flow logic
   - Task generation + JSON parsing

6. **Context builder** (`internal/context/`)
   - File reading with token estimation
   - **Summary extraction after task completion**
   - **Tiered summary inclusion based on dependencies**
   - Pruning strategy
   - Prompt template assembly

7. **Scheduler** (`internal/scheduler/`)
   - Dependency graph building
   - Parallel batch generation
   - `CanRunParallel(t1, t2)` check

### Phase 3: Execution
8. **File lock manager** (`internal/scheduler/filelock.go`)
   - flock-based locking
   - Timeout handling
   - Auto-release

9. **Executor** (`internal/executor/`)
   - Claude Code process spawning
   - Output capture
   - Completion detection
   - **Summary extraction on completion**

### Phase 4: TUI
10. **TUI screens** (`internal/tui/`)
    - Breakdown screen (questions)
    - Task confirmation screen
    - Execution progress screen
    - Review screen

## Files to Create

| File | Purpose | Complexity |
|------|---------|------------|
| `cmd/aiflow/main.go` | Entry point | Low |
| `internal/cli/root.go` | Root command + flags | Low |
| `internal/cli/start.go` | Start command | Medium |
| `internal/config/config.go` | Config struct + loading | Low |
| `internal/worktree/manager.go` | Worktree operations | Medium |
| `internal/state/state.go` | State structs | Low |
| `internal/state/persistence.go` | Save/load JSON | Low |
| `internal/breakdown/breakdown.go` | Feature breakdown orchestration | High |
| `internal/breakdown/task.go` | Task struct + utilities | Low |
| `internal/context/builder.go` | Context assembly | Medium |
| `internal/context/summary.go` | **Summary extraction + inclusion** | Medium |
| `internal/context/tokens.go` | Token estimation | Low |
| `internal/scheduler/scheduler.go` | Dependency graph + batching | High |
| `internal/scheduler/filelock.go` | File locking | Medium |
| `internal/executor/executor.go` | Claude Code invocation | Medium |
| `internal/tui/app.go` | Main TUI model | High |
| `internal/tui/breakdown.go` | Breakdown screen | Medium |
| `internal/tui/execution.go` | Execution screen | Medium |

## Dependencies (go.mod)

```go
module github.com/howell-aikit/aiflow

go 1.22

require (
    github.com/spf13/cobra v1.8.0
    github.com/charmbracelet/bubbletea v0.25.0
    github.com/charmbracelet/bubbles v0.18.0
    github.com/charmbracelet/lipgloss v0.10.0
    github.com/go-git/go-git/v5 v5.11.0
    github.com/pelletier/go-toml/v2 v2.2.4
    github.com/gofrs/flock v0.8.1
    github.com/google/uuid v1.6.0
)
```

## Verification Plan

1. **Unit tests:**
   - Scheduler: dependency graph building, batch generation
   - Context builder: token estimation, pruning, summary inclusion
   - Summary extraction: JSON parsing
   - State: save/load/resume

2. **Integration tests:**
   - Worktree creation/cleanup
   - Mock Claude Code responses for breakdown
   - Full flow with test repo

3. **Manual testing:**
   - Run `aiflow start "Add a simple feature"` on a test repo
   - Verify worktree created
   - Walk through interactive breakdown
   - Confirm parallel execution works
   - **Verify summaries extracted and included in dependent tasks**
   - Check file locking prevents conflicts
   - Resume after interruption (Ctrl+C)

## Configuration (~/.aiflow/config.toml)

```toml
worktree_dir = ".aiflow-worktrees"
max_parallel = 3
claude_code_path = ""  # Empty = use PATH
default_branch = "main"
context_max_files = 20
context_max_tokens = 8000
state_dir = "~/.aiflow/state"
lock_timeout = "5m"

# Summary inclusion settings
[summaries]
include_for_dependencies = true    # Full summaries for dependent tasks
include_for_same_feature = true    # Light summaries for non-dependent tasks
max_summary_tokens = 1000          # Limit per summary
```

## Key Differences from Ralph TUI

| Ralph TUI | aiflow |
|-----------|--------|
| 38,633 lines | ~5,500 lines (estimated) |
| 6 agents supported | Claude Code only |
| Remote instance control | Local only |
| Rate limit fallback | Simple retry |
| Subagent tracing | Not needed |
| Multi-tracker support | Simple JSON tasks |
| Bun/TypeScript | Go single binary |
| Session in same dir | Worktree isolation |
| Full context carryover | **Hybrid: fresh + structured summaries** |
