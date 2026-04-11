---
name: plan-review
description: >
  Review, validate, and prepare a plan for optimal execution by subagents or coordinated teams.
  This skill analyzes the plan, marks task complexity ([H]/[S]/[O]), determines the fastest
  execution strategy (parallel subagents, teams, mixed dispatch), and commits the reviewed plan.
  Use this skill whenever the user says "/plan-review", "review the plan", "prepare plan for execution",
  "optimize the plan", "is the plan ready", "plan readiness", or any request to validate and prepare
  an existing plan before implementation. Also triggers on "mark complexity", "execution strategy",
  "plan delegation", or "prepare for plan-approved".
  ALWAYS use this skill after /plan and before /plan-approved — it's the pre-flight check that makes execution fast.
  Do NOT use for post-implementation audits — use /plan-check for "check the plan" or "audit the plan".
  With 1M context, small/medium plans (≤15 tasks) can flow directly into /plan-approved without clearing context.
---

# Plan Review: Validate & Prepare for Execution

Review, validate, and prepare a plan for execution. This is the pre-flight check before `/plan-approved` — it ensures the plan is complete, tasks are complexity-marked, and the execution strategy maximizes throughput.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /finish
                     ↑ context clear only if context usage > 50% after plan-review
/plan → /plan-review -wt → [worktree created] → cd worktree → /plan-approved → ...
```

## Model Requirement

Run `/plan-review` on **Opus**. This skill uses sequential thinking, dependency analysis, and cross-phase parallelism detection — all tasks that benefit from deep reasoning. Switch to **Sonnet** after plan-review commits, for `/plan-approved` coordination.

## Critical Rules

These are non-negotiable:

1. **Read plan + team-execution.md in parallel.** Step 1/9 reads both via `bravros meta` + bundled reference. Never fabricate plan details.
2. **Use sequential thinking.** Call `mcp__sequential-thinking__sequentialthinking` at Step 3/9 for validation. Never skip it.
3. **Mark ALL tasks.** Every task gets `[H]`/`[S]`/`[O]` at Step 5/9. No unmarked tasks allowed.
4. **Teams are the default** for phases with `[S]`/`[O]` tasks. Parallel subagents are the default for all-`[H]` phases. Multiple teams can run in parallel across independent phases.
5. **Follow step order.** Steps build on each other.
6. **Plan file ONLY.** Do NOT modify application source code, config files, or any file outside `.planning/`. This skill edits ONLY the plan markdown file.
7. **Preserve plan structure.** Do NOT create new phases, remove phases, or reorganize the plan. "Fix issues directly" means refining task descriptions, adding missing test tasks within existing phases, and clarifying acceptance criteria — NOT redesigning the plan.

---

### Auto-mode detection

If `$ARGUMENTS` contains `--auto`:
- Set `AUTO_MODE = true`
- Strip `--auto` from `$ARGUMENTS`
- All `AskUserQuestion` calls below become no-ops (skip and return)

## Step 1/9: Parallel Data Gather

```bash
echo "📋 plan-review [1/9] reading plan + team-execution ref"
~/.claude/bin/bravros meta
```

Returns branch, base_branch, plan_file, next_num, project, team as JSON. Derive PLAN_NUM from the leading digits in `plan_file`. If $ARGUMENTS is a path or number, use that instead.

Run these **simultaneously in a single step** (do not wait between them):
1. **READ the full plan file** — required before Step 2
2. Read `references/team-execution.md` (bundled) — needed for Steps 5/9-6/9 (complexity tiers + delegation strategy)

Do NOT proceed to Step 3/9 until you have read both.

## Step 2/9: Re-Review Detection

```bash
echo "📋 plan-review [2/9] re-review detection — checking existing markers"
```

After reading the plan file, check for existing review artifacts:

1. Does the plan already have a `## Execution Strategy` section?
2. Do ALL tasks have `[H]/[S]/[O]` complexity markers?

If BOTH are true, this is a **re-review** (strategy rebuild only):
- Skip Step 3/9 (sequential thinking validation) and Step 5/9 (complexity marking) — already done
- Jump directly to Step 6/9 (determine execution strategy)
- Log: `echo "🔄 Re-review detected — rebuilding execution strategy only"`

**Safety gate:** If during Step 4 the strategy analysis discovers task markers that appear incorrect (e.g., `[H]` on a task that describes business logic or complex service integration), ABORT the fast path:
- Log: `echo "⚠️ Marker mismatch detected — running full review"`
- Return to Step 3/9 and run the full validation sequence

If EITHER condition is false (no strategy section OR unmarked tasks), proceed with the normal full review (Step 3/9 onwards).

## Step 3/9: Validate Plan

```bash
echo "📋 plan-review [3/9] running sequential thinking validation"
```

You MUST use `mcp__sequential-thinking__sequentialthinking` here. This is MANDATORY — never skip it, even for small plans. Use it to validate:

- **Completeness**: Does each phase have clear, actionable tasks?
- **Dependencies**: Are phases ordered correctly? Build a dependency chain.
- **Vague tasks**: Refine task descriptions to be specific and implementable (do NOT add new phases or restructure)
- **Missing tests**: Every phase that touches code MUST have corresponding tests — add test tasks within the existing phase if missing
- **Missing acceptance criteria**: Add if absent
- **File conflicts**: Two phases modifying same file → must be sequential or same worker
- **Feasibility**: Spot-check key codebase files (use Explore agents for deeper dives)
- **Plan size**: With 1M context, large plans are fine — the coordinator only orchestrates. No need to split.
- **Phase dependency validation**: Verify no phase depends on output from a later phase
- **Cross-phase file conflict detection**: List files touched per phase, flag overlaps explicitly
- **Stale references**: Spot-check that key files/classes referenced in the plan actually exist in the codebase (use Glob/Grep). Flag any stale references for the user.
- **Partial execution**: If any tasks are already marked `[x]`, note them as completed and adjust the execution strategy to only cover remaining `[ ]` tasks.

Refine existing tasks directly — do NOT create new phases, split phases, or restructure the plan.

## Step 4/9: Validate Tech Stack Versions (parallel with Step 3/9)

```bash
echo "📋 plan-review [4/9] validating tech stack versions"
```

The `## Tech Stack Versions` section should already exist — `/plan` auto-fills it from `sdlc detect-stack --versions` + lockfile analysis. If missing or empty:

```bash
bravros detect-stack --versions
```

Then read the project's lockfile (`composer.lock`, `package-lock.json`, `go.sum`, etc.) to supplement with exact versions of key packages. This section helps workers follow framework best practices and enables Context7 to fetch version-accurate documentation.

## Step 5/9: Mark Task Complexity + Update Frontmatter

```bash
echo "📋 plan-review [5/9] marking task complexity + updating frontmatter"
```

Update the plan frontmatter status from `todo` → `awaiting-review` at the start of this step. Add `strategy` and `reviews` fields to frontmatter at Step 7/9 (after determining strategy).

**Mark ALL tasks** in the Phases section:

| Marker | Tier | When to Use |
|--------|------|-------------|
| `[H]` | Fast/small | CRUD, config, i18n, migrations, styling, renaming, views, constants |
| `[S]` | Balanced | Business logic, services, API integration, complex tests, components |
| `[O]` | Strong reasoning | Architecture, multi-system coordination — rare |

Default to `[H]`. Escalate to `[S]` only when the task needs real reasoning. `[O]` is almost never needed.

> **Note:** Test-writing tasks are `[S]` → Sonnet. Sonnet 4.6 writes excellent tests with sufficient reasoning depth.

### Verification Phase Rule ⚠️ CRITICAL

**NEVER mark "verify + fix" phases as Leader Direct.** If a phase is labeled "verify and fix stragglers", "fix remaining failures", or similar:

1. **Split it into two distinct phases:**
   - Phase N: **Verification** (Leader Direct — just run tests, collect results, zero fixing)
   - Phase N+1: **Fix Stragglers** (Parallel Subagents — one agent per failure category)

2. **Reserve Leader Direct ONLY for purely mechanical tasks** — run pint, update a config line, bump a version number, update a constant. If there's any chance of failures to investigate, it MUST be delegated.

3. **Verification phases that may reveal failures** should always use Parallel Subagents with the note: "If verification reveals failures, coordinator dispatches N agents = N failure categories found."

This forces the plan to account for parallel fix dispatch upfront, rather than the coordinator improvising when failures appear and burning 70%+ of context debugging inline.

**Note:** This is the ONE exception to the "no new phases" rule — verification+fix phases MUST be split for correct execution.

## Step 6/9: Determine Execution Strategy

```bash
echo "📋 plan-review [6/9] determining execution strategy"
```

> **Re-review safety gate:** If this is a re-review (via Step 2/9 fast path), validate that existing `[H]/[S]/[O]` markers match task descriptions before proceeding. If any marker appears incorrect, abort fast path and run full `/plan-review` from Step 3/9.

**The goal is MAXIMUM THROUGHPUT — finish the plan as fast as possible.**

### Quick Plan Profile

Before entering the decision tree, extract:

| Metric | Value |
|--------|-------|
| Total open tasks | count `[ ]` items |
| Phase count | number of `###` phase headings |
| Max task complexity | highest marker: H / S / O |
| Parallel potential | any phases with zero file overlap? yes / no |

### Size Thresholds (override decision tree for small plans)

| Condition | Override |
|-----------|---------|
| ≤3 total open tasks, all `[H]` | → **Leader Direct** — no spawn needed |
| ≤8 total open tasks, all `[H]` | → **Single Subagent** — no team overhead |
| ≤5 total open tasks, any `[S]`/`[O]` | → **Mode C: Single Team** — team adds value for reasoning |
| >8 tasks OR any `[S]`/`[O]` with >5 tasks | → Continue to Step 6A (full decision tree) |

### Execution Modes

| Mode | Name | When |
|------|------|------|
| A | Parallel Subagents | 2+ independent phases, **all `[H]`** — fire and forget |
| B | Parallel Teams | 2+ independent phases with **any `[S]`/`[O]`** — N team-lead subagents |
| C | Single Team | 1 phase with `[S]`/`[O]` — leader gives full attention |
| D | Mixed Dispatch | Team for `[S]`/`[O]` phase + subagents for `[H]` phases simultaneously |
| E | Coordinated Team | Tasks within a team need sequential handoff |
| F | Single Subagent | 1 phase, all `[H]`, sequential |
| G | Leader Direct | ≤3 `[H]` tasks, no spawn needed |

Key constraint: each team-lead can only manage ONE team. Parallel = N separate team-leads, not 1 leader managing N teams.

### ⚠️ CRITICAL: Team vs Subagent Selection Rule

**Any phase containing `[S]` or `[O]` tasks MUST use a team-based mode (B, C, D, or E) — NEVER Mode A or F.**

- Phase has any `[S]`/`[O]` → team-lead subagent (spawns its own workers internally)
- Phase is all `[H]` → regular subagent (fire-and-forget, no team overhead)

**Mixed rounds are common:** If Round 2 has Phase A (all `[H]`) + Phase B (has `[S]`), use Mode D:
- Phase A → regular subagent (Haiku — all `[H]` tasks)
- Phase B → team-lead subagent (Sonnet for `[S]`, Opus for `[O]`, manages its own workers)

This is the #1 most common mistake — defaulting everything to "Parallel Subagents" even when phases have `[S]` tasks. A team-lead subagent gives the phase its own coordinator + workers, which produces better results for complex logic.

### Step 6A: Build Phase Dependency Graph

This is the biggest speed win — which phases can run simultaneously?

1. List each phase's **files/directories** it touches
2. Identify phases that touch **different codebases** (e.g., backend vs frontend, models vs views)
3. Identify phases with **no data dependencies** (Phase B doesn't need Phase A's output)
4. Check for **file conflicts** — two phases touching the same file must be same worker or sequential
5. Group independent phases into **parallel Rounds**

**Common parallel patterns:**
- API + Mobile/Frontend → always parallelizable (different codebases)
- Backend models + Frontend components → parallelizable
- Tests that only read code → parallelizable with everything except what they test
- Phases touching same file → combine into one worker

### File-Touch Matrix

Build an explicit file-touch matrix to identify parallelism opportunities:

```
| Phase | Files/Dirs Touched | Depends On |
|-------|-------------------|------------|
| Phase 1 | app/Models/Cavalo.php, app/Filament/Resources/CavaloResource/ | — |
| Phase 2 | app/Models/Motorista.php, app/Filament/Resources/MotoristaResource/ | — |
| Phase 3 | app/Services/FreteService.php, app/Models/Frete.php | Phase 1 |
```

**Rules:**
- If two phases have ZERO file overlap AND no data dependency → mark as parallel
- If two phases touch the same directory but different files → usually parallel (verify no shared imports)
- If two phases touch the same file → MUST be sequential or assigned to same worker
- Document the parallelism analysis explicitly — never skip this step

This matrix MUST be included in the `## Execution Strategy` section so `/plan-approved` can verify parallel dispatch is safe.

### Step 6B: Choose Mode Per Round

```
2+ independent phases (run in parallel)?
├─ YES → Any phase has [S]/[O] tasks?
│         ├─ YES → Mode B: Parallel Teams
│         └─ NO (all [H]) → Mode A: Parallel Subagents
│         ★ Mixed: [S]/[O] phase as team-lead + [H] phase as subagent — dispatched together
└─ NO (1 phase or sequential)
          ├─ Any [S]/[O] tasks?
          │   ├─ YES → Tasks within team need handoff?
          │   │         ├─ YES → Mode E: Coordinated Team
          │   │         └─ NO  → Mode C: Single Team
          │   └─ NO (all [H]) → ≤ 3 tasks?
          │               ├─ YES → Mode G: Leader Direct
          │               └─ NO  → Mode F: Single Subagent
          └─ Sequential [S]/[O] + concurrent [H] phases → Mode D: Mixed dispatch
```

**Model per worker** — the marker IS the model:
- `[H]` → `model: "haiku"` — Haiku handles all mechanical tasks (CRUD, config, migrations, styling)
- `[S]` → `model: "sonnet"` — Sonnet handles business logic, services, complex tests
- `[O]` → `model: "opus"` — Opus for architecture and multi-system reasoning (rare)
- Team-lead model = highest marker in their phase. Every Agent call MUST set the `model:` parameter explicitly.

Workers are identified by sequential IDs (worker-1, worker-2, etc.) within each team round.

### Step 6C: Maximize Parallelism

- **Dispatch all at once** — spawn every worker in ONE message
- Leader can manage ONE active team while subagents run in parallel — but NEVER 2 active teams at once
- Never serialize what can run in parallel

### Common Profiles

| # | Profile | Recommended Strategy |
|---|---------|---------------------|
| 1 | 1 phase, ≤3 tasks, all `[H]` | **Leader Direct** |
| 2 | 2+ phases, all `[H]`, zero file overlap | **Mode A: Parallel Subagents** |
| 3 | 2+ phases, any `[S]`/`[O]`, zero file overlap | **Mode B: Parallel Teams** |
| 4 | 1 phase, `[S]`/`[O]` tasks, sequential | **Mode C: Single Team** |
| 5 | 1 team needed + concurrent `[H]` phases | **Mode D: Mixed Dispatch** |

## Step 7/9: Update Plan File — Execution Strategy Section

```bash
echo "📋 plan-review [7/9] updating plan file — execution strategy section"
```

Add the `## Execution Strategy` section to the plan. The format depends on the mode chosen. Read `references/team-execution.md` for worker IDs and delegation details.

### Header Block (always present)

```markdown
## Execution Strategy

> **Approach:** `/plan-approved` with <actual strategy>
> **Total Tasks:** N (H: X, S: Y, O: Z)
> **Estimated Rounds:** N (X parallel, Y sequential)
```

### Round Formats by Mode

**Parallel Subagents (Mode A) — all `[H]` phases only:**
```markdown
### Round 1: Phase 1 + Phase 2 → Parallel Subagents (2 workers, dispatched together)
Independent phases, all [H] — Phase 1 touches `database/migrations/`, Phase 2 touches `resources/views/`.

| Phase | Mode | Model | Tasks | Notes |
|-------|------|-------|-------|-------|
| Phase 1: Migrations | Subagent | Haiku | 1.1, 1.2, 1.3 (3x[H]) | Simple schema work |
| Phase 2: Blade views | Subagent | Haiku | 2.1, 2.2 (2x[H]) | Simple tasks |
```

**Parallel Teams (Mode B) — phases with `[S]`/`[O]`:**
```markdown
### Round 2: Phase 3 + Phase 4 → Parallel Teams (2 team-leads, dispatched together)
Independent phases with [S] tasks — Phase 3 touches `app/Services/`, Phase 4 touches `app/Models/`.

| Phase | Mode | Model | Tasks | Notes |
|-------|------|-------|-------|-------|
| Phase 3: Service layer | Team-lead | Sonnet | 3.1, 3.2, 3.3 (1x[H] + 2x[S]) | Team-lead spawns workers for business logic |
| Phase 4: Model scoping | Team-lead | Sonnet | 4.1, 4.2 (2x[S]) | Team-lead spawns workers for scope logic |
```

**Single Team (Mode C):**
```markdown
### Round 2: Phase 3 → Single Team (depends on Round 1)

| Task | Model | Worker | Notes |
|------|-------|--------|-------|
| 3.1 Wire service | [S] | worker-1 | Integration logic |
| 3.2 Feature tests | [S] | worker-2 | Tests for integration |
```

**Mixed Dispatch (Mode D):**
```markdown
### Round 2: Phase 3 (team) + Phase 4 (subagent) → Mixed dispatch

| Phase | Mode | Model | Tasks | Notes |
|-------|------|-------|-------|-------|
| Phase 3: Complex service | Team | Sonnet | 3.1, 3.2 (2x[S]) | Leader manages |
| Phase 4: Config | Subagent | Haiku | 4.1, 4.2 (2x[H]) | Mechanical tasks, [H]=haiku |
```

**Coordinated Team (Mode E):**
```markdown
### Round 3: Phase 5 → Coordinated Team (handoff required)

| Task | Model | Worker | Notes |
|------|-------|--------|-------|
| 5.1 Build interface | [S] | worker-1 | Writes handoff |
| 5.2 Implement consumer | [S] | worker-2 | Waits for handoff |
```

**Leader Direct (Mode G):**
```markdown
### Round 4: Phase 6 → Leader Direct
3 trivial config changes. Tasks: 6.1 update .env.example, 6.2 add constant, 6.3 update README
```

**Also update the frontmatter in the same edit** (save a tool call):

- Set `status: approved` (from `awaiting-review`)
- Add `strategy: <chosen-mode>` (e.g., `parallel-teams`, `single-subagent`, `leader-direct`)
- Add `reviews:` array with any corrections/findings from validation (empty array `[]` if none)
- Do NOT add `phases_total`, `tasks_total`, `sessions`, or blockquote status bars — these are v1 fields

## Step 8/9: Pre-Commit Checklist

```bash
echo "📋 plan-review [8/9] verifying pre-commit checklist"
```

Verify ALL items before committing:

- [ ] ALL tasks have `[H]`/`[S]`/`[O]` complexity markers
- [ ] Each round has the correct mode label
- [ ] Parallel all-`[H]` phases use regular subagents — Mode A (fire-and-forget)
- [ ] **Every phase with `[S]`/`[O]` tasks uses a team mode (B/C/D/E) — NEVER Mode A/F**
- [ ] Parallel `[S]`/`[O]` phases use team-lead subagents — Mode B (each manages own team)
- [ ] Sequential `[S]`/`[O]` phases use Single Team — Mode C with sequential worker IDs
- [ ] Coordinated Team justified — tasks WITHIN a team need sequential handoff
- [ ] Mixed rounds dispatch team + subagents in ONE message
- [ ] Round table includes `Mode` column showing `Subagent` or `Team-lead` per phase
- [ ] Task counts per phase in round descriptions
- [ ] Model matches marker: `[H]`→Haiku, `[S]`→Sonnet, `[O]`→Opus — every Agent call sets `model:` parameter explicitly
- [ ] Cross-phase parallelism was explicitly analyzed (not skipped)
- [ ] If all-sequential: documented why no parallelism is possible
- [ ] No `[S]`/`[O]` tasks in Leader Direct rounds — Leader Direct is ONLY for all-`[H]` mechanical tasks
- [ ] Approach line reflects actual mode used
- [ ] Only the plan file in `.planning/` was modified — no application source code changes

## Step 9/9: Commit, Create Branch, and Output

```bash
echo "📋 plan-review [9/9] committing reviewed plan + creating branch"
git add .planning/ && git commit -m "📋 plan: review NNNN-<description>"
```

This review commit is the BASELINE — it contains the full task list with complexity markers and execution strategy.

After committing, create the feature branch (deferred from `/plan`):

```bash
# Create feature branch (deferred from /plan)
BRANCH_NAME=$(grep '^branch:' "$PLAN_FILE" | awk '{print $2}')
~/.claude/bin/bravros branch create "$BRANCH_NAME"
```

### Worktree Creation (if `-wt` or `--worktree` flag)

If the flag is present, create a lightweight worktree:

```bash
REPO_NAME=$(basename "$PWD")
PARENT_DIR=$(dirname "$PWD")
PLAN_NUM_SHORT=$(echo "$PLAN_NUM" | sed 's/^0*//')
WORKTREE_PATH="${PARENT_DIR}/${REPO_NAME}${PLAN_NUM_SHORT}"

bravros worktree setup "$BRANCH_NAME" --path "$WORKTREE_PATH"
command -v code &>/dev/null && code "$WORKTREE_PATH"
```

No `.env` copying, no `composer install`, no `npm` — just the isolated working directory.

### Output Summary

```
Plan Review Complete!
Plan:   .planning/NNNN-type-description-todo.md
Branch: type/description (created here — ready for implementation)
Status: Ready for Approval

Execution Rounds:
  Round 1 → Parallel Teams: Phase 1 (Sonnet) + Phase 2 (Sonnet) [spawned together]
  Round 2 → Single Team: Phase 3 (Sonnet) [depends on Round 1]

Totals: N tasks, M rounds (~X parallel savings)

[if -wt]
Worktree: /path/to/repoNN   ← cd here, then run /plan-approved
```

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: review-passed. NEXT: plan-approved"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**If lock file present (autonomous mode):** Output `STATUS: review-passed. NEXT: plan-approved` and return immediately.

**If NOT `--auto` mode and no lock file:**

⛔ **STOP. Use `AskUserQuestion` here.**

**Context clear decision** — based on actual context usage, not plan size. The coordinator only orchestrates (delegates to subagents), so even moderate remaining context is enough:

| Context used | Recommendation |
|-------------|---------------|
| < 30% | **Continue directly** — plenty of headroom for orchestration |
| 30-50% | **Suggest clear** — "Context clear recommended but you can continue" |
| > 50% | **Recommend clear** — "Clear context for maximum headroom" |

- **Question (< 30% context, no worktree):** "Plan reviewed and committed. Context is light — continuing directly to `/plan-approved`."
  - Option 1: "Continue — run /plan-approved now"
  - Option 2: "Clear context first, then /plan-approved"
  - Option 3: "Let me adjust the plan first"
- **Question (30-50% context, no worktree):** "Plan reviewed and committed. Context at ~X% — clearing recommended but optional."
  - Option 1: "Continue anyway — run /plan-approved now"
  - Option 2: "Clear context first, then /plan-approved"
  - Option 3: "Let me adjust the plan first"
- **Question (> 50% context, no worktree):** "Plan reviewed and committed. Context at ~X% — recommend clearing for maximum headroom."
  - Option 1: "Got it — clearing context now"
  - Option 2: "Continue anyway — run /plan-approved now"
  - Option 3: "Let me adjust the plan first"
- **Question (with -wt):** "Plan reviewed. Worktree ready at `/path/to/repoNN`."
  - Option 1: "Got it — opening worktree terminal now"
  - Option 2: "Let me adjust the plan first"

**If `--auto` mode:** Output the summary and return immediately. Do not prompt.

## Flags

- `--worktree` / `-wt`: Create lightweight worktree after commit
- `--no-worktree` / `-nw`: Skip worktree creation (default)
- `--auto`: Suppress all `AskUserQuestion` checkpoints. Used by `/auto-pr` and `/auto-pr-wt` when delegating to this skill. When `--auto` is present, skip the final "What's next?" prompt and return immediately after completing work.

Use $ARGUMENTS as plan file path or flags.

## Rules

- **Plan file ONLY** — do NOT modify application source code, config, or any file outside `.planning/`
- **Preserve structure** — do NOT create new phases, remove phases, or reorganize the plan (exception: splitting verify+fix phases per the Verification Phase Rule)
- **Parallel all-`[H]` phases** → regular subagents (fire-and-forget, dispatch all at once)
- **Parallel `[S]`/`[O]` phases** → team-lead subagents (each manages own team internally)
- **Sequential `[S]`/`[O]` phase** → Single Team with sequential worker IDs (worker-1, worker-2, etc.)
- **Mixed dispatch is valid** — 1 team + N subagents launched in same message
- **Coordinated Team** is for tasks WITHIN a team that need sequential handoff (not cross-phase)
- File conflict = combine into one worker or make sequential
- Always look for cross-phase parallelism first
- Use sequential thinking to analyze — don't rubber-stamp
- Refine existing tasks, preserve existing plan content
- Keep your own context lean for the `/plan-approved` that follows