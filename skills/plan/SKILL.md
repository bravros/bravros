---
name: plan
core: true
description: Create a reviewed .planning dossier folder — phases, tier markers, acceptance — ready for /orchestrate. Use on `/plan`, `/plan --worktree`, or `/plan B-NNNN` to promote a backlog item.
---

# plan

Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/plan/references/briefing.md) on demand for detailed context and instructions.

INTENT: Produce ONE reviewed folder `.planning/P-NNNN-<slug>/` for zero-translation execution by `/orchestrate`.

## Core Steps

1. **Reserve identity**: Check status table via `fold.py`, reserve `PLAN_ID=$(bravros nextid reserve plan)` (or release on abort), create `.planning/P-NNNN-<slug>/`.
2. **Interview**: Ask only diverging questions. Save closed decisions & canonical constraints in `README.md`.
3. **Write & Review**:
   - Write `README.md` following [`dossier-template.md`](file:///Users/skaisser/Sites/bravros/skills/plan/references/dossier-template.md).
   - Review inline (validate path existence, tier markers `[H]/[S]/[O]`, dependencies, and CLI smoke tests).
4. **Record & Handoff**:
   - Append `created` and `reviewed` events to `.planning/events.jsonl`.
   - Commit: `bravros commit "📋 plan: add P-NNNN <slug>" .planning/`.
   - Hand off to `/orchestrate .planning/P-NNNN-<slug>/`.

## Flags
- `--auto`: Skip interactive prompts.
- `--worktree`: Execute within an isolated worktree via [`worktree-extension.md`](file:///Users/skaisser/Sites/bravros/skills/plan/references/worktree-extension.md).
