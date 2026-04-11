---
name: plan-approved
description: >
  Execute a reviewed plan by delegating work to subagents and coordinated teams.
  Reads the Execution Strategy from the plan, dispatches workers per round, tracks task completion
  with [x] marks and timestamps, commits after each phase, and manages context for long executions.
  Use this skill whenever the user says "/plan-approved", "execute the plan", "start implementation",
  "run the plan", or any request to begin coding based on an existing reviewed plan.
  Also triggers on "let's build it", "go ahead and implement", or "start coding".
  Does NOT trigger on: "resume", "continue the plan", "pick up where we left off", "where did we stop" — use /resume for those.
  ALWAYS run after /plan-review. With 1M context, if context usage is under 30% after plan-review,
  continue directly — the coordinator only orchestrates (delegates to subagents), so it needs minimal context.
  Only clear when context is already above 50% after plan-review.
---

# Plan Approved: Execute Implementation

Execute a reviewed plan by delegating all work to subagents or coordinated teams. The leader orchestrates — it never writes code directly.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /finish
                     ↑ context clear only if context usage > 50% after plan-review
```

## Critical Rules

1. **Read + validate in one pass.** Step 1 reads the plan via `bravros meta`, validates review markers, detects TDD mode, and extracts the Execution Strategy — all from the same read. If `[H]/[S]/[O]` markers or `## Execution Strategy` are missing → STOP.
2. **Leader NEVER implements code.** Always delegate to subagents or teams. If you're reading source files, writing code, or running tests — STOP. You are violating this rule. Dispatch a worker instead.
3. **Skip team-execution.md** — the plan already has everything from /plan-review. Only read it if Execution Strategy is incomplete.
4. **Follow the Execution Strategy exactly** — mode, workers, model tiers per round.
5. **Parallel dispatch = ONE message.** For Parallel Subagents, Parallel Teams, or Mixed: dispatch ALL workers in a single message.
6. **One active team at a time.** Parallel Teams = N team-lead subagents (each autonomous), not 1 coordinator managing N teams.
7. **Commit plan marks + code together.** Never commit code without updating task marks. Never leave tasks unmarked.
8. **Every phase ends with /commit.** Workers commit their own phase code, coordinator commits plan updates to memory.

## ⛔ Dispatch Enforcement — THE MOST IMPORTANT SECTION

**The #1 failure mode is the coordinator doing work itself instead of spawning agents.** This wastes context, runs sequentially, and defeats the entire execution strategy.

### Self-Check (ask yourself before EVERY action)

> "Am I about to read a source file, write code, or run a test?"
> → YES = STOP. Dispatch a worker via the `Agent` tool instead.
> → The ONLY exception is Mode G (Leader Direct, ≤3 trivial `[H]` tasks).

### Minimum Dispatch Rules

| Plan size | Minimum dispatch |
|-----------|-----------------|
| ≤3 tasks, all `[H]` | Leader Direct OK (Mode G) |
| 4-5 tasks | At least 1 subagent |
| 6+ tasks | At least 2 concurrent subagents per round |
| Any `[S]`/`[O]` | MUST be dispatched to agent — never Leader Direct |

### What the Agent Tool Call Looks Like

For each worker, call the `Agent` tool with `subagent_type: "general-purpose"`. The prompt MUST include:

1. **Phase details** — copy the exact tasks from the plan
2. **File paths** — every file the worker will touch
3. **Context** — relevant interfaces, contracts, patterns from Context Pack
4. **Test commands** — exact test runner commands (from `sdlc meta --field stack.test_runner` or project CLAUDE.md)
5. **Worker Completion Protocol** — the full protocol from Step 4
6. **Working directory** — `cd /path/to/project` as the first instruction

Example dispatch (2 parallel subagents):
```
Agent call 1: "Implement Phase 1 — Service layer refactoring"
  prompt: "You are worker-1. cd /Users/.../project. Your phase: [paste phase details]. Files: [...]. When done: [paste Worker Completion Protocol]."

Agent call 2: "Implement Phase 2 — Frontend components"
  prompt: "You are worker-2. cd /Users/.../project. Your phase: [paste phase details]. Files: [...]. When done: [paste Worker Completion Protocol]."
```

**Both calls go in ONE message — they run simultaneously.** This is 2x faster than sequential.

**Model selection per worker — the marker IS the model:**
- `[H]` → `model: "haiku"` — Haiku for all mechanical tasks (CRUD, config, migrations, styling)
- `[S]` → `model: "sonnet"` — Sonnet for business logic, services, tests, components
- `[O]` → `model: "opus"` — Opus for architecture and multi-system reasoning (rare)
- Every Agent call MUST set the `model:` parameter explicitly — Rule 19 in `bravros audit` enforces this and will block Agent calls with wrong or missing model.

> **Rule 19 enforcement:** `bravros audit` blocks Agent calls where the `model:` parameter doesn't match task markers. `[H]`=haiku, `[S]`=sonnet, `[O]`=opus.

Example with model selection:
```
Agent call 1: "Phase 1 — Config files only" (≤2 config-only [H] tasks)
  model: "haiku"
  prompt: "You are worker-1..."  ← [H] tasks = Haiku

Agent call 2: "Phase 2 — Business logic service" (has [S] tasks)
  model: "sonnet"
  prompt: "You are worker-2..."  ← [S] tasks = Sonnet

Agent call 3: "Phase 3 — Architecture decisions" (has [O] tasks)
  model: "opus"
  prompt: "You are worker-3..."  ← [O] tasks = Opus (explicit, always)
```

### Failure Signs (stop and correct if you notice these)

- ❌ You've been running for 5+ minutes without dispatching any agent
- ❌ You're reading source files beyond the plan file and team-execution.md
- ❌ You're writing or editing code files
- ❌ You're running tests (other than pre-dispatch baseline)
- ❌ You're investigating errors beyond a single pass to categorize them

---

## Step 1/5: Read Plan + Validate

Run these **in parallel** (one bash call each):

```bash
# Check 1: auto-pr lock
if [ -f ".planning/.auto-pr-lock" ]; then
  echo "⚠️ auto-pr is currently active (lock file found). Yielding."
  exit 0
fi

# Check 2: get plan metadata
echo "📋 plan-approved [1/5] reading plan file"
~/.claude/bin/bravros meta
```

If the lock file exists, STOP and inform the user. Do not proceed — conflicting execution will cause merge conflicts.

Returns branch, base_branch, plan_file, next_num, project, team, git_remote as JSON.

> **Note:** `sdlc meta` may return stale plans if `-todo.md` files exist with completed status. Verify by reading the plan file directly.

**READ the full plan file**, then analyze it in ONE pass (no extra tool calls):

### 1A: Phase Status
- Count `[x]` vs `[ ]` tasks to determine status
- All `[x]` → **SKIP** | Mix of `[x]` and `[ ]` → **RESUME from first `[ ]`** | All `[ ]` → **EXECUTE**

### 1B: Validate Plan Was Reviewed
Check for BOTH — if EITHER is missing, STOP with `AskUserQuestion`:

| Check | What to look for |
|-------|-----------------|
| Complexity markers | `[H]`, `[S]`, or `[O]` on tasks in Phases section |
| Execution strategy | `## Execution Strategy` section in plan body |

Valid input statuses: `todo`, `awaiting-review`, or `approved` — all are fine. Status is set to `in-progress` at Step 2.

### 1C: Detect TDD Mode
Check for `mode: tdd` in frontmatter or `> **Mode:** TDD` in the plan. If active:

| Phase type | Constraint |
|------------|-----------|
| Phase 0 (RED) | Workers write ONLY tests — zero implementation code. ALL tests must fail before marking complete. |
| GREEN phases | Workers implement ONLY what failing tests require. Run targeted tests after each task. |
| REFACTOR phase | Run the project's test coverage command (e.g. pest --coverage, jest --coverage, pytest --cov, go test -cover) — must reach 100% coverage. |

TDD enforcement rules are appended to the Worker Completion Protocol (Step 4).

### 1D: Read Execution Strategy
Extract rounds, modes, model tiers, and worker assignments from the `## Execution Strategy` section. **Skip reading `team-execution.md`** — the plan already has everything.

**Quick model reference (inline — no file read needed):**
- `[H]` → `model: "haiku"` — mechanical tasks (CRUD, config, migrations, styling)
- `[S]` → `model: "sonnet"` — business logic, services, tests, components
- `[O]` → `model: "opus"` — architecture and multi-system reasoning (rare)
- Every Agent call MUST set `model:` explicitly — Rule 19 in `bravros audit` enforces this

## Step 2/5: Update Status

```bash
echo "📋 plan-approved [2/5] updating plan status"
```

1. Update frontmatter `status: in-progress`
2. Set frontmatter `session: "${CLAUDE_SESSION_ID}"` (single field, not array) — this enables resume via `claude -r <session-id>`

## Step 3/5: Commit Baseline

```bash
echo "📋 plan-approved [3/5] committing baseline"

# Project code baseline (if staged changes):
~/.claude/bin/bravros commit "📋 plan: start execution NNNN"
# or on resume:
~/.claude/bin/bravros commit "📋 plan: resume execution NNNN"

# Plan file status update → project repo:
git add .planning/ && git commit -m "📋 plan: start execution NNNN"
```

Creates the baseline snapshot: project code via `bravros commit`, plan state via git. This is critical for the commit chain guarantee — every state is recoverable from git history.

## Step 4/5: Execute Rounds

```bash
echo "📋 plan-approved [4/5] starting execution — following strategy from plan"
```

**Context Protection:** Execute ONE round at a time. After each round: update plan → test → commit → assess context. With 1M context, most plans complete in a single session — suggest compacting only after 5+ rounds or when context visibly degrades.

Read the Execution Strategy section from the plan. Execute EXACTLY the mode it specifies.

### Mode Execution Reference

**Mode A — Parallel Subagents** (2+ independent all-`[H]` phases)
- Call `Agent` tool N times in **ONE message** — they run simultaneously
- Each prompt must be fully self-contained (phase details, files, test commands)

**Mode B — Parallel Teams** (2+ independent phases with `[S]`/`[O]`)
- Call `Agent` tool N times in **ONE message** — each spawned subagent IS a team-lead
- Each prompt instructs the subagent to: create a team (team-bifrost, team-asgard, etc.), assign workers (bifrost-1, bifrost-2, etc.), implement the phase
- Coordinator dispatches all and waits — does NOT manage individual teams mid-task

**Mode C — Single Team** (1 sequential `[S]`/`[O]` phase, coordinator actively manages)
- Use `TeamCreate` to create one team. Assign workers with sequential IDs (worker-1, worker-2, etc.)
- Coordinator assigns tasks, monitors, messages workers mid-task if blocked
- Only ONE active team at a time

**Mode D — Mixed Dispatch** (1 team + concurrent `[H]` subagents)
- In ONE message: create the team AND call `Agent` for the `[H]` subagent(s)
- Team runs under coordinator attention; subagents fire-and-forget simultaneously

**Mode E — Coordinated Team** (tasks within a team need sequential handoff)
- Same as Single Team, but worker-1 writes a handoff block in the plan before worker-2 starts
- Coordinator re-prompts worker-2 with the handoff pasted in

**Mode F — Single Subagent** (1 phase, all `[H]`, sequential)
- One `Agent` call. Self-contained prompt. Coordinator waits for completion.

**Mode G — Leader Direct** (≤ 3 `[H]` tasks total)
- Coordinator handles tasks directly — no spawn.

### Worker Completion Protocol

Every worker prompt (subagent or team) MUST include these instructions:

```
WHEN YOU FINISH YOUR PHASE:

1. Get timestamp: date "+%Y-%m-%dT%H:%M"

2. Run targeted tests and confirm they pass:
   - Run the project's test runner (from `sdlc meta --field stack.test_runner` or project CLAUDE.md)
   - On failure: categorize by root cause, report to coordinator — do NOT attempt multi-category fixes alone

2b. If the project requires 100% coverage on new files (check project CLAUDE.md or memory):
   - Run coverage on NEW files only using the project's coverage tool:
     - Laravel: `herd coverage vendor/bin/pest --coverage --filter="TestName" | grep "NEW_CLASS"`
     - Node.js: `npx jest --coverage --collectCoverageFrom="src/path/to/new-file.ts"`
     - Python: `pytest --cov=path.to.new_module tests/test_new.py`
     - Go: `go test -coverprofile=cover.out ./path/to/new/... && go tool cover -func=cover.out`
   - If any new file is below 100%, add tests before reporting completion
   - Skip this step if the project has no coverage requirement

3. Format and commit your CODE changes (do NOT push — coordinator pushes after verifying):
   a. Run the project's formatter if available (e.g. pint --dirty, prettier --write, ruff format, gofmt)
   b. git add -A -- ':!.env' ':!*.key' ':!*-api-key*'
      (do NOT exclude paths already in .gitignore like node_modules/, dist/ — git add -A respects .gitignore automatically)
      If git add -A fails, fall back to: git add <specific-files-you-changed>
   c. git commit -m "<emoji> <type>: <description>"

4. Report completion to coordinator — the coordinator verifies, marks tasks [x], and pushes.

HARD RULES:
- NEVER leave code uncommitted — commit BEFORE reporting completion
- NEVER use raw git commit without running the project's formatter first
- NEVER add AI signatures to commits
- NEVER stage .env, *-api-key, or credential files
```

### Worker Timeout and Partial Completion

- **Worker timeout:** If a worker has not returned after 10 minutes, check its status via `TaskOutput`. If stuck or hung, stop it via `TaskStop` and dispatch a replacement worker for the remaining tasks.
- **Partial completion:** If a worker returns having completed only some of its assigned tasks (e.g., 3 of 5 done), do NOT re-dispatch the entire phase. Instead:
  1. Verify which tasks are marked `[x]` in the plan file
  2. Dispatch a new worker for ONLY the remaining `[ ]` tasks
  3. Include in the new worker's prompt: "Tasks 1-3 are already complete. You are implementing tasks 4-5 only."

### Worker Prompt Template

Every worker prompt MUST include these context sections. The coordinator gathers this info BEFORE dispatching:

```
WORKER CONTEXT BLOCK (include in every worker prompt):

## Project
- Working directory: /path/to/project
- Phase: [paste exact phase details from plan]
- Files to touch: [list every file path]

## Tech Stack Versions
[From the plan's Tech Stack Versions section — auto-detected by /plan-review]
- Framework: [project's framework and version]
- Key packages: [any relevant package versions]
- Runtime: [language version if relevant]

## Code Patterns (read BEFORE implementing)
- Read one existing file of the same type for patterns (e.g., building a new controller? Read an existing one first)
- Follow the existing naming conventions, imports, and structure exactly

## Test Commands
- Test runner: $(bravros meta --field stack.test_runner)
- Targeted: run the project's test runner (from `sdlc meta --field stack.test_runner` or project CLAUDE.md)
- Filtered: run the project's test runner with filter (e.g. pest --filter, jest --testPathPattern, pytest -k, go test -run)

## Test File Location Convention (Laravel — for other stacks, follow the project's convention)
- Filament resources → tests/Feature/Filament/{Resource}Test.php
- Services → tests/Feature/Services/{Service}Test.php
- Custom pages → tests/Feature/{Page}Test.php
- Livewire components → tests/Feature/Livewire/{Component}Test.php
- Models → tests/Unit/Models/{Model}Test.php

For non-Laravel projects, follow the stack's convention (e.g., `__tests__/` for JS, `*_test.go` for Go, `tests/` for Python).

## Commit Rules
- Format code: run the project's linter/formatter (e.g. pint, prettier, ruff, gofmt — see project CLAUDE.md)
- Stage: git add -A -- ':!.env' ':!*.key' ':!*-api-key*'
  (do NOT exclude paths already in .gitignore like node_modules/, dist/ — git add -A respects .gitignore automatically)
  If git add -A fails, fall back to: git add <specific-files-you-changed>
- Commit: git commit -m "<emoji> <type>: <description>"
- Do NOT push — coordinator pushes after verification
- No AI signatures

[paste Worker Completion Protocol here]
```

The coordinator MUST:
1. Read the project's lockfile (`composer.lock`, `package-lock.json`, `go.sum`, etc.) or use `sdlc meta --field stack` to extract framework versions
2. Read one existing file of the same type the worker will create (e.g., building a new service? Read an existing service first)
3. Read relevant domain model files to include schema/relationships in the prompt
4. Include the Tech Stack Versions section from the plan (added by /plan-review)

### Agent Quality Rules

These rules are battle-tested patterns from real execution failures. Include the relevant ones in worker prompts based on the task type. Examples use Laravel/PHP syntax — adapt for your project's stack.

#### Migration Ordering (include in worker prompts for migration tasks)

When creating multiple related migrations (applies to any framework with timestamped migrations):
1. After creating migrations, verify ordering: list migration files sorted by name
2. FK-dependent tables MUST have LATER timestamps than their parent tables
3. If a child table (with foreign keys) sorts before its parent, rename the parent migration to an earlier timestamp
4. Same-second timestamp collisions cause alphabetical sorting — `affiliate_links` before `affiliates` = FK failure

#### Parallel Test Isolation (include in ALL worker prompts with test tasks)

When tests run in parallel, cross-contamination between processes is the default:
1. NEVER assert exact counts on unscoped queries — other parallel processes create records too
2. ALWAYS scope assertions to factory-created records (e.g., filter by a specific parent ID)
3. NEVER use random enum values in fixtures when the test depends on specific behavior — pin the value explicitly
4. Use factory-created records as scope filters to isolate from parallel test pollution

#### Date Math in Tests (include in worker prompts with time-based test logic)

Date arithmetic has boundary surprises across all languages:
1. NEVER use relative date subtraction to create "previous period" test data — e.g., March 30 minus 1 month may not give Feb 28
2. Use explicit dates for predictable test data (e.g., `new Date(2025, 0, 15)`, `Carbon::create(2025, 1, 15)`, `datetime(2025, 1, 15)`)
3. For period-based tests, use the service's own period calculation to get exact boundary dates
4. Prefer mid-month dates (10th-20th) in test fixtures to avoid boundary issues entirely

#### Factory/Fixture Self-Sufficiency (include in worker prompts with fixture tasks)

When a fixture assigns roles, permissions, or references related records:
1. Ensure referenced records exist before assigning (e.g., `firstOrCreate` pattern)
2. Fixtures may run in tests that don't seed global data — never assume related records exist
3. Same applies to any fixture that references related records via relationships

#### Model Hook / Lifecycle Impact Assessment (include when tasks add model lifecycle hooks)

When adding model lifecycle hooks (e.g., `saving`, `creating`, `beforeSave`, `pre_save`):
1. These hooks fire on EVERY save/create across the entire codebase — not just the file you changed
2. After implementing, grep for ALL usages of the model in tests
3. Run ALL affected test files, not just the targeted ones
4. Any test that creates the model with non-conforming data will break
5. Lifecycle hooks are the #1 source of cascading test failures — always do a full impact scan

#### Semantic Impact Rules (include in worker prompts for permission/role/auth tasks)

When tasks involve permissions, roles, ACLs, or authorization changes:
1. **Expanding a permission (adding access) can break tests** — a test asserting forbidden/403 will pass when the user now has the permission
2. **Before implementing**, search for forbidden/unauthorized assertions in the test suite. Identify every test that relies on the OLD permission boundary
3. **For each such test**, verify the test uses a user WITHOUT the expanded permission
4. **Never silently "fix" a forbidden test by granting permission** — flag it to the coordinator if you're unsure whether the restriction is intentional

#### Package Route Registration (include in worker prompts when installing packages with routes)

When installing packages that register routes (any framework):
1. After installation, verify routes are accessible: run the framework's route listing command filtered to the expected path (e.g., `php artisan route:list --path=<expected-path>` for Laravel, or equivalent)
2. Check for conflicts with existing route registrations — wildcard routes from documentation packages (LaRecipe, Scribe, etc.) can silently capture new package paths
3. If a conflict is detected, configure the package to use a non-conflicting base path before proceeding

### Pre-Dispatch Validation (NEW — reduces errors)

Before dispatching each round, the coordinator MUST verify:

1. **File conflict check** — No two workers in the same round touch the same file. If they do, reassign to one worker or make sequential.
2. **Dependency check** — All phases in this round have their prerequisites completed (previous round's tasks all `[x]`).
3. **Context pack** — Each worker prompt includes: the specific phase details, file paths to touch, relevant interfaces/contracts from the Context Pack, and targeted test commands.
4. **Test baseline** — Run the targeted tests for the upcoming phase BEFORE dispatching. If tests already fail, fix first — workers should start from green.

Skip pre-dispatch validation only for Mode G (Leader Direct) where overhead exceeds value.

### Immediate Dispatch Rule ⚠️ CRITICAL

**The leader is an orchestrator, not a debugger.** When a verification step reveals failures:

1. Read the diagnostic output (test results, error log) **ONCE** — a single pass
2. Categorize failures by distinct root cause (e.g., "3 auth failures, 2 missing factory fields, 1 route typo")
3. Dispatch **N agents in parallel** — one per failure category — in ONE message
4. **Do NOT spend more than 3 tool calls investigating before dispatching**

If a pre-existing diagnostic file exists (failing-tests.md, captured test output), read it ONCE, count distinct failure categories, and dispatch immediately. Investigation is a worker task, not a coordinator task.

### Context Budget Discipline

- **After 5+ rounds of execution**, or if context quality visibly degrades, suggest the user compact and resume. With 1M context, most plans complete without needing a compact.
- When context is heavy and failures remain, **prefer dispatching agents over inline investigation** — agents protect the leader's remaining context.
- The leader should NEVER spend more than 10% of its context on investigation. If you've read more than 3 files to diagnose an issue, you've already spent too much — dispatch a worker instead.

### Round Loop

For each incomplete round:

1. **Pre-validate** — Run the pre-dispatch checks above.
2. **Dispatch** — Execute the mode from the Execution Strategy. For parallel modes, send ALL workers in ONE message. Every worker prompt MUST include the Worker Completion Protocol.
3. **Wait** — Let all workers/teams complete. Confirm each worker has committed and marked tasks `[x]`.
4. **Verify** — `git diff`, run targeted tests using the project's test runner (from `sdlc meta --field stack.test_runner`)
4b. **Data validation** — If the completed phase involved data import/seeding, verify counts:
   - Query the database to check record counts against plan expectations (e.g. tinker for Laravel, dbshell for Django, psql/sqlite3 for raw SQL)
   - If counts are off by >20%: log warning, dispatch investigation agent
   - If counts are 0 (complete failure): dispatch fix agent before proceeding
   - This prevents cascading errors from silent data import failures
5. **On failures: apply Immediate Dispatch Rule** — categorize in ONE pass, dispatch N agents in parallel. Do NOT debug inline.
6. **Mark tasks (MANDATORY)** — ALWAYS read the plan file after each round. Check every task assigned to workers in this round. If ANY task is still `[ ]` but the worker reported completion, mark it `[x]` with timestamp NOW. Do not rely on workers to mark — the coordinator is the final authority. If marking fails with "File has been modified since read", re-read the plan file and retry once. This handles race conditions when background agents modify the plan concurrently.
6b. **Normalize timestamps** — Scan the plan file for any non-ISO timestamps (DD/MM/YYYY pattern) and auto-convert to ISO 8601:
    ```bash
    # Coordinator safeguard: normalize non-ISO timestamps before committing plan marks
    PLAN_FILE=".planning/$(~/.claude/bin/bravros meta | jq -r '.plan_file')"
    if [[ "$OSTYPE" == "darwin"* ]]; then
      sed -i '' -E 's|([0-9]{2})/([0-9]{2})/([0-9]{4})|\3-\2-\1T00:00|g' "$PLAN_FILE"
    else
      sed -i -E 's|([0-9]{2})/([0-9]{2})/([0-9]{4})|\3-\2-\1T00:00|g' "$PLAN_FILE"
    fi
    ```
    This runs AFTER workers complete, as a coordinator safeguard to enforce ISO 8601 format.
7. **Update plan** — mark completed tasks with `[x]` and timestamp in format: `✅ YYYY-MM-DDTHH:MM` appended inline after the task text. Example: `- [x] [H] Create migration ✅ 2026-04-06T04:10`. Do NOT use HTML comments (`<!-- -->`) for timestamps — they must be visible in rendered markdown.
8. **Commit** —
   ```bash
   /commit                                              # project code changes (if any coordinator fixes)
   git add .planning/ && git commit -m "📋 plan: round X complete NNNN"       # plan progress
   ```
10. **Assess context** — After 5+ rounds or if context quality degrades: use `AskUserQuestion` to suggest compact + resume. With 1M context, most plans complete without needing this.

### Post-Round Error Recovery

If a worker reports test failures or incomplete work:

1. **Categorize in ONE pass** — Read the output once. Group by root cause.
2. **Dispatch N agents** — One per failure category, in ONE message. Include specific error context per agent.
3. **Verify fix** — Run targeted tests again before marking complete.
4. **Update plan** — Mark the fixed tasks, add a note about the recovery.

**NEVER investigate failures inline.** The coordinator reads results, categorizes, dispatches. Workers investigate and fix.

#### Agent Execution Error Recovery

If an agent returns an error, empty result, or the message `[Tool result missing due to internal error]`:

1. **Do NOT re-dispatch the entire round.** Re-dispatch ONLY the failed agent.
2. Use the exact same prompt as the original dispatch.
3. Prepend to the prompt: `"Previous attempt failed — retry. [paste original prompt]"`
4. All other agents that completed successfully are NOT re-run.
5. If the retry also fails, categorize the failure (infra error vs. logic error) and report to the user via `AskUserQuestion` before attempting a third dispatch.

## Step 5/5: On Completion

```bash
echo "📋 plan-approved [5/5] all phases complete"
```

- Update frontmatter `status: completed` (v1 compat: also accepts `Completed`)
- Shutdown any active team
- Run final targeted tests
- `/commit` for final plan state (all `[x]` marks and timestamps visible)
- `git add .planning/ && git commit -m "✨ feat: complete NNNN-<description>"`

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: execution-complete. NEXT: plan-check"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**If lock file present (autonomous mode):** Output `STATUS: execution-complete. NEXT: plan-check` and return.

**If no lock file:**

Use `AskUserQuestion`:
- **Question:** "Implementation complete. What's next?"
- **Option 1:** "Run /plan-check" — Audit implementation vs plan
- **Option 2:** "Done for now" — I'll run /plan-check manually

## Rules

- Fix test failures before marking a phase complete
- Only commit via `/commit` (coordinator) or inline formatter+git commands (workers — see Worker Completion Protocol)
- No AI signatures — hook rejects them
- For full test suite: ask the user to run in a separate terminal
- ALWAYS delegate work — leader orchestrates, never implements
- Keep all task marks and timestamps visible — never collapse during execution
- When in doubt about scope: check the plan, not memory. The plan is the single source of truth.