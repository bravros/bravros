---
name: auto-pr
description: >
  Fully autonomous SDLC pipeline — zero user intervention. Opus 4.6 runs the entire workflow
  from plan through PR, handles review feedback loops, and stops only when the PR is ready
  for human merge. Use this skill whenever the user says "/auto-pr", "auto pr",
  "autonomous flow", "just do everything", "full auto", "hands off", "run it all",
  or any request to run the complete pipeline without checkpoints or user decisions.
  Also triggers on "no intervention", "auto pipeline", "unattended flow", "fire and forget flow",
  "do everything and leave me a PR", or "I'm going to sleep just build it".
  This is the zero-touch version of /flow — same pipeline, no pauses.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Auto PR: Fully Autonomous SDLC Pipeline

Run the entire SDLC pipeline with zero user intervention. Opus 4.6 makes all decisions autonomously — planning, reviewing, executing, creating the PR, addressing review feedback, and looping until the PR is clean. Stops only when done, leaving a final recommendation comment on the PR.

```
/auto-pr <description>
  → plan → review → execute → check → quality sweep → green gate → PR → review loop → report
  Zero AskUserQuestion calls. Zero pauses. One command → PR ready for merge.
```

## Overview

This skill automates all 8 stages of the shared SDLC pipeline with zero human intervention. The coordinator makes all decisions autonomously based on context usage, plan complexity, code quality checks, and test results.

**Read `references/pipeline.md` for the shared pipeline stages and context management rules.**

**Read `references/mode-autonomous.md` for autonomous decision-making, quality sweep/green gate, and review loop behavior.**

## Critical Rules

1. **NEVER use AskUserQuestion** — every decision is handled autonomously. The `--auto` flag on delegated skills suppresses their checkpoints too.
2. **NEVER merge the PR** — stops after creating/fixing the PR. User decides when to merge.
3. **Commit after every stage** — full git history for recovery.
4. **Green Gate + Quality Sweep MUST pass before PR** — no exceptions. Max 3 fix rounds per gate.
5. **Loop review→fix max 3 times** — prevent infinite loops on stubborn review feedback.
6. **Branch is created by /plan-review** — `/plan` commits to the current branch; `/plan-review` creates the feature branch. The `--auto` flag propagates to both delegated skills.
7. **Mandatory checkpoint echoes** — every stage must echo: `echo "🤖 [auto-pr:N] description"` for audit hook validation.
8. **Effort budget per phase:** Max 2 fix rounds per phase before moving on and noting the issue.
9. **PR must be merge-ready on creation** — Quality Sweep + Green Gate is the primary quality bar, not the review loop.
10. **If context critical (>85%):** Compact and continue — the pipeline must complete.
11. **NEVER invoke /auto-pr unless the user EXPLICITLY requested it.** If the user asked for /plan-review, /plan-approved, or any interactive skill — run that exact skill. Do NOT substitute /auto-pr as an "optimization." The user chose interactive mode for a reason.

## Flags

- `--from <stage>`: Start from specific stage (plan, review, execute, check, pr, review-loop)
- `--no-review`: Skip the review loop entirely (just create the PR and stop)
- `--max-cycles N`: Override the max review cycle count (default: 3)

Use $ARGUMENTS as the task description or flags.
