---
name: auto-pr-wt
description: >
  Fully autonomous SDLC pipeline in an isolated git worktree — zero user intervention,
  zero git conflicts with other terminals. Creates a lightweight worktree (no Herd URL,
  no VS Code, no SSL), runs the full auto-pr pipeline inside it, and stops when the PR
  is ready for merge. Use this skill whenever the user says "/auto-pr-wt", "auto pr worktree",
  "parallel auto-pr", "auto-pr in worktree", or any request to run the autonomous pipeline
  in an isolated worktree. Also triggers on "isolated auto-pr", "worktree auto", "parallel auto",
  "run auto-pr in isolation", "don't touch my branch", "run in parallel", "parallel pipeline",
  or when running multiple auto-pr instances on the same repo.
  Supports `--auto-merge` flag for batch pipelines to merge PR immediately after creation.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Auto PR WT: Autonomous Pipeline in Isolated Worktree

Run the full autonomous pipeline inside an isolated git worktree. Same zero-touch autonomy as `/auto-pr`, but each instance gets its own working directory so multiple pipelines can run on the same repo without git conflicts.

```
/auto-pr-wt <description>
  → worktree → plan → review → execute → check → quality sweep → green gate → PR → (optional) review loop → cleanup → DONE
  Lightweight worktree (git isolation only — no Herd, no VS Code, no SSL).
  Zero AskUserQuestion calls. Zero pauses. Main repo working directory never modified.
```

## Overview

This skill combines the autonomous pipeline from `/auto-pr` with worktree isolation. All 10 stages execute autonomously inside an isolated worktree, preventing git conflicts when running parallel pipelines on the same repo.

**Read `references/pipeline.md` for the shared pipeline stages and context management rules.**

**Read `references/mode-autonomous.md` for autonomous decision-making, quality sweep/green gate, and review loop behavior.**

**Read `references/worktree-setup.md` for worktree creation, dependency installation, and cleanup.**

## Critical Rules

1. **NEVER use AskUserQuestion** — all decisions are autonomous.
2. **NEVER modify main repo HEAD or working directory** — use `git fetch` + remote refs only.
3. **NEVER merge the PR** (unless `--auto-merge` is set by `/auto-merge` skill).
4. **Auto-cleanup worktree after PR** — code is safely on remote branch (use `--keep-worktree` to skip).
5. **Worktree is lightweight** — no Herd, no VS Code, no SSL. Uses `sdlc worktree setup` / `sdlc worktree cleanup`.
6. **When `--auto-merge` is set** — merge PR immediately after creation, skip review loop, return merge status.
7. **Mandatory checkpoint echoes** — every step must echo for audit hook validation.
8. **Green Gate + Quality Sweep MUST pass before PR** — no exceptions.
9. **Max 3 review cycles** — prevent infinite loops.
10. **If context critical (>85%):** Compact and continue — the pipeline must complete.
11. **NEVER invoke /auto-pr-wt unless the user EXPLICITLY requested it.** If the user asked for /plan-review, /plan-approved, or any interactive skill — run that exact skill. Do NOT substitute /auto-pr-wt as an "optimization." The user chose interactive mode for a reason.

## Flags

- `--from <stage>`: Start from specific stage
- `--no-review`: Skip the review loop
- `--max-cycles N`: Override max review cycle count (default: 3)
- `--no-install`: Skip composer/npm install in worktree
- `--keep-worktree`: Do not auto-cleanup worktree after PR creation
- `--auto-merge`: Merge PR immediately after creation, skip review loop. Used by `/auto-merge` to prevent cascading merge conflicts.

Use $ARGUMENTS as the task description or flags.
