---
name: tdd-review
description: >
  TDD variant of plan-review — restructures the plan to follow Red→Green→Refactor cycle.
  Use this skill whenever the user says "/tdd-review", "tdd plan", "test-driven review",
  "red green refactor", or any request to prepare a plan for TDD execution.
  Also triggers on "tdd mode", "write tests first", "test-first approach", "tdd planning",
  "prepare plan for TDD", or "write tests before code".
  Injects Phase 0 (all tests written first, failing), GREEN implementation phases, and final
  Coverage & Refactor phase targeting 100% coverage.
---

# TDD Review: Red→Green→Refactor Plan

TDD variant of `/plan-review`. Restructures the plan to follow Red→Green→Refactor: all tests written first (failing), implementation makes them pass, coverage phase reaches 100%.

This skill ONLY restructures the plan file — NEVER write actual test or implementation code.

```
/plan → /tdd-review → [clear context] → /plan-approved → /plan-check → /pr → /finish
```

## Critical Rules

- You MUST read the plan file AND `references/team-execution.md` at Step 1. NEVER fabricate plan details.
- You MUST use `mcp__sequential-thinking__sequentialthinking` at Step 1. NEVER skip.
- You MUST inject Phase 0 (test writing) before ALL implementation phases.
- Phase 0 worker MUST confirm tests FAIL before marking complete.
- NEVER allow implementation in Phase 0 — write tests only.
- You MUST add a final Coverage phase targeting 100%.
- Mark ALL tasks `[H]`/`[S]`/`[O]`. NEVER leave unmarked tasks.
- Phase 0 test tasks MUST be derived from the plan's actual models, endpoints, and features — NEVER use generic placeholders.
- Every Phase 0 test task MUST map to a corresponding GREEN implementation task, and vice versa.

## Testing Conventions

Follow the project's test framework conventions (from `bravros meta --field stack.test_runner` or project CLAUDE.md):
- **NEVER mock what you can test** — real models, real DB, only mock external HTTP
- Use the project's idiomatic test style (e.g., Pest `it()` for PHP, `test()`/`it()` for Jest, `def test_` for pytest)
- Parameterized tests where the framework supports them (Pest datasets, Jest `each`, pytest parametrize)

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/8: Parallel Data Gather + Validate ⚠️ MANDATORY

```bash
echo "📋 tdd-review [1/8] gathering plan data and validating"
```

Run these **simultaneously**:
1. `~/.claude/bin/bravros meta` — get plan metadata
2. Read the full plan file (from meta output)
3. Read `references/team-execution.md` for execution mode selection

Then use `mcp__sequential-thinking__sequentialthinking` to validate completeness, dependencies, testability, file conflicts. During validation, **extract a list of all models, endpoints, services, and features** from the plan — this list drives Phase 0 test naming.

⛔ DO NOT proceed without reading the plan file AND team-execution.md.

## Step 2/8: Update Session

```bash
echo "📋 tdd-review [2/8] updating session metadata"
```

Set frontmatter `session: "${CLAUDE_SESSION_ID}"` (single field — enables resume via `claude -r <session-id>`).

## Step 3/8: Mark Task Complexity

```bash
echo "📋 tdd-review [3/8] marking task complexity"
```

Mark ALL tasks: `[H]` (simple), `[S]` (medium), `[O]` (complex/rare). Default to `[H]`.

## Step 4/8: Inject Phase 0 — Write All Tests (RED) ⚠️ MANDATORY

```bash
echo "📋 tdd-review [4/8] injecting phase 0 (RED)"
```

Insert Phase 0 at TOP of Phases section. Derive test class names directly from the plan's models, endpoints, and features extracted in Step 1. Each test task must target a specific entity from the plan.

Example (adapt to actual plan content):

```markdown
### Phase 0: Write All Tests 🔴 (RED)

> **TDD:** Write ALL tests before any implementation. Tests MUST fail at the end of this phase.

- [ ] [S] Write Feature tests: CreateOrderTest, UpdateOrderStatusTest (from Phase 1 tasks)
- [ ] [S] Write Unit tests: OrderServiceTest, PricingCalculatorTest (from Phase 2 tasks)
- [ ] [H] Run tests → verify ALL fail (failures expected)
- [ ] [H] Commit: `🧪 test: add failing tests for [feature] (RED)`
```

**Naming rule:** For each implementation task in the plan that produces testable behavior, create a corresponding test task in Phase 0. Use the format `[Model]Test`, `[Feature]Test`, or `[Endpoint]Test` — derived from the plan, never generic placeholders.

## Step 5/8: Annotate Implementation Phases (GREEN) + Verify RED→GREEN Mapping

```bash
echo "📋 tdd-review [5/8] annotating implementation phases (GREEN)"
```

Add `🟢 (GREEN)` annotation to each implementation phase.

**After annotating, verify 1:1 mapping:** Walk through every Phase 0 test task and confirm there is a GREEN implementation task that will make those tests pass. If a test task has no corresponding implementation task, add one. If an implementation task has no corresponding RED test, add a test task to Phase 0. Document the mapping as a comment block in the plan:

```markdown
<!-- RED→GREEN mapping:
  Phase 0: CreateOrderTest → Phase 1: Create Order model + migration + controller
  Phase 0: OrderServiceTest → Phase 2: Implement OrderService
-->
```

## Step 6/8: Inject Final Phase — Coverage & Refactor ⚠️ MANDATORY

```bash
echo "📋 tdd-review [6/8] injecting final coverage & refactor phase"
```

```markdown
### Phase N+1: Coverage & Refactor 🔵 (REFACTOR)

> **TDD:** Target 100% coverage on files touched by this feature.

- [ ] [S] Run coverage — identify uncovered lines
- [ ] [S] Write additional tests for uncovered branches
- [ ] [H] Confirm all green
- [ ] [H] Run the project's formatter (e.g. pint, prettier, ruff, gofmt)
- [ ] [H] Commit: `🧪 test: complete 100% coverage (REFACTOR)`
```

## Step 7/8: Determine Execution Strategy + Update Header

```bash
echo "📋 tdd-review [7/8] determining execution strategy"
```

Phase 0 (RED) is ALWAYS Round 1. Coverage is ALWAYS last round. Implementation phases follow standard mode selection.

Update plan header:
```markdown
> **Mode:** TDD
> **Coverage Target:** 100%
> **Status:** Approved
```

## Step 8/8: Pre-Commit Checklist + Commit

```bash
echo "📋 tdd-review [8/8] pre-commit checklist"
git add .planning/ && git commit -m "📋 plan: tdd-review NNNN-<description>"
```

⛔ **STOP. Use `AskUserQuestion`:**
- "Got it — clearing context now"
- "Let me adjust the test list first"

## Rules

- Phase 0 is ALWAYS first round
- Coverage phase is ALWAYS last round
- Workers in Phase 0 write ONLY tests
- Workers in GREEN phases implement ONLY what failing tests require
- Professional worker naming: worker-1, worker-2, team-bifrost, team-asgard (see team-execution.md for full codename list)

Use $ARGUMENTS as plan file path if provided.