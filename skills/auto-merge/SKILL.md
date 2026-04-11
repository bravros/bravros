---
name: auto-merge
description: >
  Execute multiple plans sequentially in a single session — loops auto-pr-wt for plans N through M
  with auto context compaction, merge chain automation, and crash recovery.
  Use this skill whenever the user says "/auto-merge", "auto merge", "run plans 2 to 6",
  "execute all plans", "sequential plans", or any request to run multiple plans in one session.
  Also triggers on "multi-plan", "batch execute", "run remaining plans", "plans N through M",
  "batch pipeline", "run plans 3 4 and 5", "I have N plans to execute",
  "execute plan N through plan M", "run all backlogs", "promote and execute backlogs",
  or "batch the remaining plans". This is the multi-plan orchestrator for building MVPs.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Auto Merge: Multi-Plan Sequential Execution

Run multiple plans sequentially in a single session. Orchestrates `/auto-pr-wt` (worktree-isolated) in a loop with context management, merge chain automation, and crash recovery.

```
/auto-merge N-M [--no-merge] [--effort-budget Nm]
  → For each plan N to M:
    → worktree → pipeline → auto-merge → context check → next plan
  Auto-merges PRs between plans by default (prevents cascading conflicts).
  Zero AskUserQuestion calls. One command → N plans executed.
```

## Overview

This skill orchestrates multiple autonomous pipelines sequentially. Each plan runs in its own worktree inside the full `/auto-pr-wt` pipeline, with automatic merging and context management between plans.

**Read `references/pipeline.md` for shared pipeline stages and context management.**

**Read `references/mode-autonomous.md` for autonomous decision-making and review loop behavior.**

**Read `references/worktree-setup.md` for worktree creation, installation, and cleanup.**

**Read `references/batch-loop.md` for multi-plan orchestration, homolog mirror strategy, and crash recovery.**

## Critical Rules

1. **NEVER use AskUserQuestion** — fully autonomous.
2. **NEVER merge to main without PR** — every merge goes through a PR.
3. **Context compaction between plans** — if context > 60% after a plan, compact before next.
4. **Skip completed plans** — if plan status = `Completed`, skip it.
5. **Resume from last incomplete** — on crash/restart, detect last incomplete plan and resume.
6. **Homolog is a rolling mirror of main** — reset to origin/main after each plan's merge (safe only in sequential batch context).
7. **Always use `/auto-pr-wt` for each plan** — never use `/auto-pr`. Worktree isolation prevents `.planning/` conflicts.
8. **Each plan gets its own branch** — never reuse branches across plans.
9. **Effort budget per plan** — if `--effort-budget` is set, each plan gets that budget independently.
10. **Mandatory checkpoint echoes** — every step must echo for audit hook validation.

## Flags

- `N-M`: Plan range (required). E.g., `2-6` for plans 0002-0006.
- `--no-merge`: Disable auto-merge between plans — leave PRs open for manual review (default: merge is ON).
- `--effort-budget Nm`: Max effort per plan. E.g., `30m` for 30 minutes.
- `--skip-completed`: Skip completed plans (default: on).
- `--from N`: Start from plan N (skip earlier plans regardless of status).

Use $ARGUMENTS as plan range and flags.
