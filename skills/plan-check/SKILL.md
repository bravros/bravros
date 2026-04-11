---
name: plan-check
description: >
  Audit implementation against the plan — compare planned vs actual changes, detect orphaned test
  references, fix checkbox mismatches, and sync frontmatter counts. Use this skill whenever the
  user says "/plan-check", "check the plan", "audit the plan", "verify implementation", "compare
  plan vs code", or any request to validate that what was built matches what was planned.
  Also triggers on "orphaned tests", "plan audit", "check task marks", "did we implement everything",
  or "sync frontmatter counts".
  ALWAYS run after /plan-approved and before /pr — this is the quality gate.
---

# Plan Check: Audit Plan vs Implementation

Analyze and update the plan file for the current branch, comparing planned vs actual implementation.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

**Sonnet 4.6** — this skill performs mechanical/scripted operations that don't require deep reasoning.

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish
```

## Critical Rules

- You MUST read the plan file first. NEVER fabricate implementation status.
- Follow steps in order. DO NOT skip or reorder steps.
- **AUDIT ONLY** — Do NOT modify application or feature code. Only update the plan file, fix orphaned test references, and sync metadata.
- **Do NOT re-implement any incomplete tasks.** Flag them as missing in the report and leave as `[ ]`. The user decides whether to implement or descope.
- **False-positive prevention:** Before unmarking a completed `[x]` task as `[ ]`, verify via `git diff` that the planned file was truly NOT modified. If a file was modified but differently than planned, keep `[x]` and note the deviation — do not revert to `[ ]`.

### Auto-mode detection

If `$ARGUMENTS` contains `--auto`:
- Set `AUTO_MODE = true`
- Strip `--auto` from `$ARGUMENTS`
- All `AskUserQuestion` calls below become no-ops (skip and return)

## Step 1/7: Gather Plan + Diffs (parallel)

Run: `echo "📋 plan-check [1/7] reading plan context + diffs"`

Run these **simultaneously in a single step** (do not wait between them):

1. `~/.claude/bin/bravros meta` — returns branch, base_branch, plan_file, project as JSON
2. `~/.claude/bin/bravros context --diffs` — returns commits, changed files, diff stat, AND per-file diffs

If $ARGUMENTS is a plan path or number, locate that file directly instead of using plan_file from meta.

**READ the full plan file** (after meta returns the path) — DO NOT proceed without reading it.

## Step 2/7: Compare Plan vs Implementation

Run: `echo "📋 plan-check [2/7] comparing plan vs implementation"`

- Was every planned task implemented?
- Are `[x]`/`[ ]` marks accurate?
- Unplanned files modified? (document why)
- Planned files NOT modified? (document why)

## Step 3/7: Detect Deleted Tasks ⚠️ CRITICAL

Run: `echo "📋 plan-check [3/7] detecting deleted tasks"`

**Agents sometimes delete `[ ]` tasks they couldn't solve instead of reporting failure.** Use the CLI to compare the plan at plan-review time vs now — no temp files needed.

```bash
# Single command: auto-detects "plan: review" commit, diffs task lists
DELETED_COUNT=$(~/.claude/bin/bravros plan-tasks --diff auto --field deleted_count 2>/dev/null)
echo "Deleted tasks: $DELETED_COUNT"
```

If `deleted_count > 0`, extract the deleted tasks:

```bash
# List deleted tasks as JSON array
~/.claude/bin/bravros plan-tasks --diff auto --field deleted
```

### Flag deleted tasks

If any tasks were removed:
- **List each deleted task** in the report (Step 7/7)
- **Re-add them** to the plan as `[ ]` with a note: `(restored by plan-check — removed during execution)`
- These must be implemented or explicitly marked as descoped by the user

This is a hard failure — deleted tasks indicate an agent tried to hide incomplete work.

## Step 4/7: Grep Test Suite for Orphaned References ⚠️ CRITICAL

Run: `echo "📋 plan-check [4/7] grepping test suite for orphaned references"`

**This step catches test files that existed at plan-review time, referenced removed behaviors, but were NOT modified during implementation.**

### 1. Find the plan-review baseline commit

```bash
# Reuse the baseline commit from sdlc plan-tasks (auto-detected in Step 3/7)
PLAN_REVIEW_COMMIT=$(git log --oneline | grep "plan: review" | head -1 | awk '{print $1}')
echo "Plan-review baseline: $PLAN_REVIEW_COMMIT"
```

### 2. Extract removed patterns from the diffs

From the diffs gathered in Step 1, identify significant removed patterns (lines starting with `-`):
- Old route params, removed properties/fields, old method calls, old URL structures, old validation rule names
- **Minimum pattern length: 4+ characters.** Exclude generic words (`name`, `id`, `type`, `data`, `test`, `user`). Use specific identifiers only (e.g., `old_field_name`, `legacyEndpoint`, `validateOldRule`).

Use **specific, non-generic patterns** to avoid false positives.

### 3. Grep the plan-review commit for each pattern

```bash
git grep -l "REMOVED_PATTERN" $PLAN_REVIEW_COMMIT -- "tests/"
```

### 4. Cross-check against modified files

Flag any test file that contained the removed pattern at plan-review time AND was NOT modified during implementation. These are **orphaned test references**.

Fix all orphaned references before proceeding to the audit commit.

## Step 5/7: Update Plan File

Run: `echo "📋 plan-check [5/7] updating plan file"`

- Fix `[x]`/`[ ]` mismatches
- Add timestamps: `date "+%Y-%m-%dT%H:%M"`
- **ALWAYS add a `## Plan Check` section** at the end of the plan file with a summary line, even when 0 mismatches. This marker is required for /pr to detect that plan-check was run. Format:
  ```
  ## Plan Check
  Audited YYYY-MM-DDTHH:MM — X/Y tasks implemented, N mismatches fixed, N deleted restored, AC X/Y verified.
  ```
- Keep existing content — add, don't delete
- Do NOT add session entries or blockquote status bars — v2 plans use single `session` field set by /plan-approved

### Verify Acceptance Criteria

If the plan has an `## Acceptance Criteria` section, verify each item against the actual implementation:
- Test each criterion (run commands, check files, confirm behavior)
- Mark verified items `[x]` with timestamp: `- [x] Criterion ✅ YYYY-MM-DDTHH:MM`
- Leave items `[ ]` if they fail — note what failed
- Report any failed AC items in Step 7/7

## Step 6/7: Audit Commit ⚠️ MANDATORY

Run: `echo "📋 plan-check [6/7] audit commit — full task list with all marks visible"`

Count mismatches fixed, deleted tasks restored, and orphaned refs fixed during this audit to build a descriptive commit message:

```bash
# If orphaned test fixes or other code changes were made, run /ship first
# Then always commit the updated plan to the project repo:
git add .planning/ && git commit -m "🧹 chore: plan check NNNN — fixed N mismatches, restored N deleted tasks, fixed N orphaned refs"
```

⛔ The plan file MUST have the full task list with all `[x]` marks and timestamps before pushing.

## Step 7/7: Report

Run: `echo "📋 plan-check [7/7] plan check complete"`

```
Plan Check Complete:
  - Planned items: X/Y implemented
  - Deleted tasks: N (restored — agents removed instead of implementing)
  - Additional items: N (beyond plan)
  - Missing items: N (not implemented)
  - Acceptance Criteria: X/Y verified
  - Files planned: X | Files modified: Y
  - Status: [All matched / Discrepancies found]
```

**If NOT `--auto` mode:**

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: audit-passed. NEXT: pr"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**If lock file present (autonomous mode):** Output `STATUS: audit-passed. NEXT: pr` and return.

**If no lock file:**

⛔ **STOP. You MUST use `AskUserQuestion` tool here.**

- **Question:** "Plan audited. What's next?"
- **Option 1:** "Run /pr" — Push and create the pull request
- **Option 2:** "I have more changes" — Continue working, run /plan-check again when done

**If `--auto` mode:** Output the summary and return immediately. Do not prompt.

## Flags

- `--auto`: Suppress all `AskUserQuestion` checkpoints. Used by `/auto-pr` and `/auto-pr-wt` when delegating to this skill. When `--auto` is present, skip the final "What's next?" prompt and return immediately after completing work.

Use $ARGUMENTS as plan file path if provided, otherwise auto-detect from branch.