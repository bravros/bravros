---
name: flow
description: >
  Auto-chain the full SDLC workflow with checkpoints and pauses for review.
  Use this skill whenever the user says "/flow", "run the full workflow",
  "auto chain", "start to finish", "full pipeline", "plan and execute",
  or any request to run the complete plan-to-finish pipeline automatically.
  Also triggers on "chain skills", "workflow pipeline", "run everything",
  "take this from plan to PR", "full dev cycle", or "run plan through finish".
  NOT for autonomous/zero-intervention flows (use /auto-pr instead).
  Sister skills: /auto-pr (autonomous), /auto-pr-wt (autonomous + worktree), /auto-merge (multi-plan).
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Flow: Interactive Auto-Chain SDLC Workflow

Orchestrate the full plan-to-finish pipeline with automatic skill chaining and mandatory review checkpoints.

**Instead of 10 manual skill invocations, run one `/flow` and let it guide you through with pauses for your decisions.**

## Overview

This skill orchestrates stages 1-7 of the shared SDLC pipeline with **3 mandatory checkpoints** where it pauses and asks what you'd like to do next. All other stages flow automatically.

**The checkpoint-driven approach:**
```
Stage 1 (plan) → Stage 2 (review) → Checkpoint A (user decides)
→ Stage 3 (execute) → Stage 4 (check) → Checkpoint B (user decides)
→ Stage 5 (PR) → Checkpoint C (user decides)
→ Stage 6 (review loop) → Stage 7 (report)
```

**Read `references/pipeline.md` for the shared pipeline stages and context management rules.**

**Read `references/mode-interactive.md` for the 3 checkpoint behaviors and user prompts.**

## Usage

```bash
/flow <description>              # Start from scratch — runs /plan first
/flow --from plan-review         # Pick up from a specific stage
/flow --from plan-approved       # Skip to execution (plan already reviewed)
```

## Rules

- **All 3 checkpoints are mandatory** — never skip Checkpoint A, B, or C (read mode-interactive.md for exact behavior)
- **Use AskUserQuestion for every checkpoint** — never auto-proceed without asking
- **Context breaks are conditional** — only recommended for large plans (>15 tasks); small/medium plans flow directly
- **Search memory at entry** — past plans inform the current one
- **Suggest /quick for small tasks** — don't over-engineer
- **Suggest /hotfix for emergencies** — don't slow down urgent fixes
- **Monitor context usage** — proactively warn about context limits
- **Each stage is idempotent** — safe to re-run with `--from`
- **Branch is created by /plan-review** — `/plan` commits the plan file to the current branch; `/plan-review` creates the feature branch after reviewing and committing

## Flags

- `--from <stage>`: Start from specific stage (plan, plan-review, plan-approved, plan-check, pr, finish)
- `--wt`: Use worktree mode (passed to plan-review)

Use $ARGUMENTS as the task description or flags.