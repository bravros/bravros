# Shared Pipeline Architecture

This document defines the core SDLC pipeline architecture shared across flow, auto-pr, auto-pr-wt, and auto-merge skills.

## Pipeline Stages

All four flow variants execute the same core stage sequence:

| # | Stage | Skill Delegated | Checkpoint Echo | What it Does | Resume Detection |
|---|-------|-----------------|-----------------|--------------|------------------|
| 1 | **Plan** | `/plan` | `echo "🤖 [skill:1] creating plan"` | Create plan document from description | Check for `plan_file` in sdlc meta |
| 2 | **Review** | `/plan-review` | `echo "🤖 [skill:2] reviewing plan"` | Validate, mark complexity [H]/[S]/[O], set execution strategy | Check plan status: `awaiting-approval` |
| 3 | **Execute** | `/plan-approved` | `echo "🤖 [skill:3] executing plan"` | Run all phases, dispatch workers, commit code | Check plan status: `approved` or `in-progress` |
| 4 | **Check** | `/plan-check` | `echo "🤖 [skill:4] auditing implementation"` | Verify tasks complete, check acceptance criteria | Check completion status in plan |
| 5 | **PR** | `/pr` | `echo "🤖 [skill:5] creating pull request"` | Create and push PR to base branch | Check for open PR on branch |
| 6 | **Review Loop** | `/address-pr` (async) | `echo "🤖 [skill:6] starting review loop"` | Poll for review feedback, dispatch fixes, loop max 3x | Check PR comment count |
| 7 | **Report** | (coordinator) | `echo "🤖 [skill:7] pipeline complete"` | Post final report comment, output summary | Final stage always completes |

**Note:** Each skill-name varies per caller:
- `/flow` uses `[flow:N]`
- `/auto-pr` uses `[auto-pr:N]`
- `/auto-pr-wt` uses `[auto-pr-wt:N]`
- `/auto-merge` uses `[auto-merge:N]`

## Context Management Rules

The pipeline monitors context usage and makes decisions about continuing vs. breaking:

```
Context < 30% after stage 2 (review):   ✅ Continue directly — full pipeline in ONE session
Context 30-50% after stage 2:           ⚠️ Suggest clear, but continuing works fine
Context 50-70% after stage 3 (execute): ⚠️ Recommend clear before stage 4
Context > 70%:                          ⚠️ Warn user — context at limit
Context > 85%:                          🔴 FORCE checkpoint — clear before next stage
```

**Rules:**
- The coordinator only orchestrates (minimal context needs). Most of the context is consumed by worker delegation.
- Small/medium plans (<15 tasks) typically flow in a single session.
- Large plans (>15 tasks) may require a context break after plan-review.
- Autonomous modes (auto-pr, auto-pr-wt, auto-merge) continue at ≥50% context — they don't ask the user, they decide.

## Entry Point Detection

When the pipeline starts, determine which stage to enter:

```bash
echo "🤖 [skill:N] determining entry point"
~/.claude/bin/bravros meta 2>/dev/null
```

This returns:
- `plan_file`: path to active plan (if any)
- `status`: plan status (awaiting-approval, approved, in-progress, completed)
- `branch`: feature branch name

**Resume logic:**

| Condition | Start Stage |
|-----------|-------------|
| No plan file exists | **Stage 1** — /plan (new task) |
| Plan exists, status = `awaiting-approval` | **Stage 2** — /plan-review (user approved plan) |
| Plan exists, status = `approved` | **Stage 3** — /plan-approved (execution ready) |
| Plan exists, status = `in-progress` | **Stage 3** — /plan-approved (resume from last incomplete phase) |
| Plan exists, status = `completed` | **Stage 5** — /pr (check if PR exists; if not, create) |
| `--from <stage>` flag provided | Jump directly to that stage |

## Checkpoint Echo Format

Every step MUST start with an echo statement. This allows audit hooks and external monitoring to track progress.

```bash
echo "🤖 [skill-name:N] description"
```

**Format rules:**
- `skill-name` is determined by the caller (flow, auto-pr, auto-pr-wt, auto-merge)
- `N` is the stage number (1-7)
- `description` is a concise action description
- Emojis are encouraged for clarity (🤖 for agent work, 📋 for data, ⚠️ for warnings)

**Examples:**
```bash
echo "🤖 [flow:1] creating plan"
echo "🤖 [auto-pr:3] executing plan"
echo "🤖 [auto-pr-wt:5] verifying tests"
echo "🤖 [auto-merge:2] pre-flight checks"
```

## Worker Completion Protocol

When delegating to worker agents (in Stage 3: Execute), workers must follow this protocol:

1. **Accept plan file path and phase description** — read the plan to understand context
2. **Run targeted tests only** — DO NOT run full test suite (that's Stage 4)
3. **Commit work via `/ship` skill** — this creates a commit message in the correct format
4. **Mark tasks as complete in plan** — update `[x]` checkboxes as work finishes
5. **On failure:** Return detailed error logs — DO NOT retry indefinitely
6. **After 2 fix rounds:** Move on and note the issue — don't get stuck on one phase

Workers use the same context rules as the coordinator — if context approaches 85%, compact and continue rather than stopping.

## State Detection and Resumption

The pipeline can resume at any point by detecting the current plan state:

```bash
# Read plan file and extract:
STATUS=$(grep '^status:' "$PLAN_FILE" | head -1 | awk '{print $2}')
CURRENT_PHASE=$(grep -E '^\## Phase' "$PLAN_FILE" | grep -v '\[x\]' | head -1)
COMPLETED_TASKS=$(grep -c '^\- \[x\]' "$PLAN_FILE")
TOTAL_TASKS=$(grep -c '^\- \[' "$PLAN_FILE")
```

Use these to:
- Skip completed phases
- Resume from the next incomplete task
- Update progress display
- Calculate effort remaining

## Git Integration

The pipeline assumes:
- Feature branches named: `feat/<description-slug>`
- Base branches: `homolog` (if exists) or `main`
- All commits are made with proper messages and author info
- `.planning/` directory tracks plan state

**Branch creation:** Done by `/plan` skill. `/auto-pr-wt` wraps this in `git worktree add` for isolation.

**Merge strategy:**
- feat → homolog → main (via PR)
- Each merge is a separate PR (never direct commits to main)
- `/auto-merge` resets homolog to main after each plan to prevent drift

## Error Handling

If a stage fails:

1. **Log the error** — include full error message and context
2. **Commit what you have** — `git add . && git commit -m "🩹 hotfix: partial recovery"`
3. **Continue or stop?**
   - Interactive mode (/flow): ask the user
   - Autonomous mode (auto-pr, auto-pr-wt, auto-merge): continue to next stage and note the issue
4. **Final report:** List all issues encountered, recommend fixes

**Catastrophic failure rule:** If a stage fails and recovery is impossible (e.g., merge conflict, test infrastructure down), commit what exists, create the PR with a failure note, and proceed to final report. A partial PR is better than no PR.

## References

- **mode-interactive.md** — Checkpoint A, B, C with AskUserQuestion (for /flow)
- **mode-autonomous.md** — Quality Sweep, Green Gate, auto-deploy (for /auto-pr and /auto-pr-wt)
- **worktree-setup.md** — Worktree creation and cleanup (for /auto-pr-wt)
- **batch-loop.md** — Multi-plan orchestration and merge chain (for /auto-merge)
