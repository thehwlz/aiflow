# aiflow Roadmap: Adaptive Specification Flow

## Overview

aiflow is a tool that orchestrates Claude Code to implement features through an adaptive, conversational specification flow. It breaks down features into parallelizable tasks, executes them using Claude Code subagents, and manages the git workflow automatically.

---

## ✅ Implemented

### Core Flow
- [x] **Adaptive Q&A** - Conversational specification with options + "Other"
- [x] **Project detection** - Detect empty vs existing projects
- [x] **Recommended options** - Mark recommended choices with descriptions
- [x] **Parallel task groups** - Tasks grouped for concurrent execution
- [x] **Task review UI** - Visual review of generated tasks with parallelization info

### Types & Config
- [x] `ProjectType` (empty/existing)
- [x] `ConversationQuestion` with `QuestionOption` (label, description, recommended)
- [x] `SpecConversation` for tracking Q&A history
- [x] `ParallelGroup` field on tasks
- [x] `SpecConfig` with safety limit

---

## 🚧 To Implement

### 1. Commits Per Task (Critical)
**Purpose:** Atomic commits enable easy rollback if something goes wrong.

**Implementation:**
- After each task completes successfully, create a commit
- Commit message format: `feat(<scope>): <task-title>`
- Store commit SHA in task state for rollback reference
- On task failure, offer: "Rollback to last good commit?"

**Files to modify:**
- `internal/executor/executor.go` - Add commit after task completion
- `internal/state/state.go` - Add `CommitSHA` field to Task
- `internal/tui/execution.go` - Show commits in progress view

---

### 2. CLAUDE.md Updates
**Purpose:** Learn from implementation and update project knowledge automatically.

**Implementation:**
- After all tasks complete, analyze what was built
- Extract patterns, conventions, and architectural decisions
- Append to or create `CLAUDE.md` in project root
- Sections to update:
  - **Patterns Used** - e.g., "Auth uses middleware pattern"
  - **Key Files** - e.g., "Auth logic in internal/auth/"
  - **Conventions** - e.g., "Errors wrapped with fmt.Errorf"
  - **Dependencies** - New packages added

**Files to modify:**
- `internal/breakdown/breakdown.go` - Add prompt for CLAUDE.md generation
- `internal/executor/executor.go` - Call CLAUDE.md update after completion
- New file: `internal/docs/claudemd.go` - CLAUDE.md management

---

### 3. Skills Creation
**Purpose:** Generate reusable Claude Code skills for project-specific tasks.

**Implementation:**
- After feature completion, identify reusable patterns
- Generate skill definitions in `.claude/skills/` or project config
- Example skills to generate:
  - `/auth-check` - Verify auth implementation is correct
  - `/add-endpoint` - Add new API endpoint following project patterns
  - `/add-migration` - Create database migration

**Files to modify:**
- New file: `internal/skills/generator.go` - Skill generation logic
- `internal/executor/executor.go` - Trigger skill generation post-completion

---

### 4. Check Global Skills Before Planning
**Purpose:** Leverage existing Claude Code skills in implementation plans.

**Implementation:**
- Before generating task breakdown, query available skills
- Check: `.claude/skills/`, global Claude Code skills, MCP tools
- Include skill usage instructions in task descriptions
- Example: "Use `/test` skill to run tests after implementation"

**Files to modify:**
- `internal/breakdown/breakdown.go` - Add skills discovery to prompt
- New file: `internal/skills/discovery.go` - Find available skills

---

### 5. Technical Debt Check Task
**Purpose:** Prevent redundant code and encourage reuse.

**Implementation:**
- Add automatic task to every plan: "Technical Debt Check"
- This task runs BEFORE implementation:
  - Search for existing code that can be reused
  - Identify similar patterns already in codebase
  - Flag potential redundancies
- This task runs AFTER implementation:
  - Check for code duplication
  - Suggest refactoring opportunities
  - Verify consistent patterns

**Task template:**
```json
{
  "title": "Pre-implementation: Check for reusable code",
  "description": "Before implementing, search codebase for existing utilities, patterns, or components that can be reused. Update plan if reusable code found.",
  "priority": 0,
  "parallel_group": "analysis"
}
```

**Files to modify:**
- `internal/breakdown/breakdown.go` - Auto-inject tech debt tasks
- `internal/breakdown/templates.go` - Tech debt check prompts

---

### 6. Merge/PR Options
**Purpose:** Flexible git workflow after completion.

**Implementation:**
- After execution, offer options:
  1. **Create PR** (Recommended) → Then offer to merge
  2. **Merge to main** → Direct merge
  3. **Keep on branch** → Manual handling later

- PR flow:
  ```
  Create PR → "PR #42 created" → "Merge now?" → Yes/No
  ```

**Files to modify:**
- `internal/tui/completion.go` - New completion screen
- `internal/git/pr.go` - PR creation and merge logic
- `internal/git/merge.go` - Branch merge logic

---

### 7. Automatic Rollback
**Purpose:** Recover gracefully from failed tasks.

**Implementation:**
- On task failure, show options:
  1. **Rollback to last commit** - `git reset --hard <sha>`
  2. **Retry task** - Run the same task again
  3. **Skip and continue** - Mark failed, proceed
  4. **Abort run** - Stop execution entirely

**Files to modify:**
- `internal/executor/executor.go` - Add rollback logic
- `internal/tui/execution.go` - Show rollback options on failure

---

### 8. Resume Support
**Purpose:** Continue interrupted runs.

**Implementation:**
- Persist run state to disk after each task
- Command: `aiflow resume [run-id]`
- On resume:
  - Load state from disk
  - Show completed tasks
  - Continue from first pending task

**Files to modify:**
- `internal/state/persistence.go` - Already exists, ensure atomic writes
- `internal/cli/resume.go` - New resume command
- `internal/tui/breakdown.go` - Handle resumed state

---

### 9. Dry-Run Mode
**Purpose:** Preview tasks without execution.

**Implementation:**
- Flag: `aiflow start --dry-run`
- Shows full task breakdown
- Displays estimated changes (files, dependencies)
- No actual execution or git changes

**Files to modify:**
- `internal/cli/start.go` - Add --dry-run flag
- `internal/tui/breakdown.go` - Dry-run specific view

---

### 10. Conflict Detection
**Purpose:** Warn about potential file conflicts between parallel tasks.

**Implementation:**
- Before execution, analyze task file dependencies
- Warn if parallel tasks write to same files
- Suggest: "Move to sequential?" or "Proceed anyway?"

**Files to modify:**
- `internal/breakdown/validation.go` - Add conflict detection
- `internal/tui/breakdown.go` - Show conflict warnings

---

### 11. Progress Persistence
**Purpose:** Allow closing terminal without losing progress.

**Implementation:**
- Save state after each task completion
- Background tasks continue even if TUI exits
- `aiflow status` shows running tasks
- `aiflow attach [run-id]` reconnects to running execution

**Files to modify:**
- `internal/executor/executor.go` - Detached execution mode
- `internal/cli/status.go` - Show running tasks
- `internal/cli/attach.go` - Reconnect to execution

---

### 12. Cost/Token Estimation
**Purpose:** Show estimated API usage before starting.

**Implementation:**
- Estimate tokens per task based on:
  - Task description length
  - Number of files to read/write
  - Historical averages
- Show before execution: "Estimated: ~50k tokens ($0.15)"

**Files to modify:**
- `internal/breakdown/estimation.go` - Token estimation logic
- `internal/tui/breakdown.go` - Show estimate in review

---

### 13. Desktop Notifications
**Purpose:** Alert user when long-running tasks complete.

**Implementation:**
- Use OS notification APIs
- Notify on: completion, failure, needs input
- Configurable in settings

**Files to modify:**
- `internal/notify/notify.go` - Cross-platform notifications
- `internal/config/config.go` - Notification settings

---

### 14. Task Editing
**Purpose:** Modify task descriptions before execution.

**Implementation:**
- In review phase, press `e` to edit selected task
- Opens inline editor or $EDITOR
- Can modify: title, description, dependencies, parallel group

**Files to modify:**
- `internal/tui/breakdown.go` - Add edit mode
- `internal/tui/editor.go` - Inline task editor

---

### 15. Summary Export
**Purpose:** Export changes as markdown for documentation.

**Implementation:**
- After completion, generate summary:
  - What was built
  - Files changed
  - Decisions made
  - How to use the feature
- Export to: clipboard, file, or PR description

**Files to modify:**
- `internal/summary/export.go` - Summary generation
- `internal/tui/completion.go` - Export option

---

## Implementation Priority

### Phase 1: Core Reliability
1. Commits per task
2. Automatic rollback
3. Resume support

### Phase 2: Intelligence
4. Check global skills before planning
5. Technical debt check task
6. CLAUDE.md updates

### Phase 3: Git Workflow
7. Merge/PR options with merge-after-PR
8. Conflict detection

### Phase 4: UX Polish
9. Dry-run mode
10. Task editing
11. Progress persistence
12. Desktop notifications

### Phase 5: Extras
13. Cost/token estimation
14. Skills creation
15. Summary export

---

## Architecture Notes

### Execution Flow
```
start
  → detect project type
  → adaptive Q&A
  → check global skills ← NEW
  → inject tech debt tasks ← NEW
  → generate task breakdown
  → validate (conflicts, dependencies)
  → user review & edit
  → execute tasks
    → for each task:
      → run with Claude Code subagent
      → commit changes ← NEW
      → update state
      → on failure: offer rollback ← NEW
  → post-execution:
    → update CLAUDE.md ← NEW
    → generate skills ← NEW
    → tech debt final check ← NEW
  → completion options (PR/merge/keep) ← NEW
  → summary export ← NEW
```

### State Machine
```
PhaseInput → PhaseDetecting → PhaseConversation → PhaseReview
    ↓              ↓                ↓                 ↓
  (quit)    (detect type)    (Q&A loop)      (edit tasks)
                                  ↓                 ↓
                            PhaseGenerating → PhaseExecution
                                                   ↓
                                            PhaseCompletion
                                                   ↓
                                            (PR/merge/done)
```

---

## Testing Strategy

### Unit Tests
- `breakdown/*_test.go` - Task parsing, validation
- `state/*_test.go` - State persistence, transitions
- `git/*_test.go` - Git operations

### Integration Tests
- Full flow tests with mock Claude responses
- Git workflow tests (commit, rollback, merge)
- TUI snapshot tests

### E2E Tests
- Real execution against test repositories
- PR creation flow
- Resume/rollback scenarios

---

## Notes

- All git operations should be non-destructive by default
- Always confirm before force operations
- Maintain backward compatibility with existing runs
- Log all Claude Code invocations for debugging
