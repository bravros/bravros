---
name: plan
core: true
description: Create a reviewed .planning dossier folder — phases, tier markers, acceptance — ready for /orchestrate. Use on `/plan`, `/plan --worktree`, or `/plan B-NNNN` to promote a backlog item.
---

# plan

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: Produce ONE reviewed folder `.planning/P-NNNN-<slug>/` for zero-translation execution by `/orchestrate`.

## Core Steps

1. **Reserve identity**:
   - Fetch latest homolog: `git fetch origin homolog`
   - Switch to `homolog` branch locally (or use worktree).
   - Reserve ID and create folder-plan: `PLAN_ID=$(bravros nextid reserve plan --slug "$SLUG")` (which creates `.planning/P-NNNN-<slug>/` and seeds `PLAN.md`).
   - Commit and push reservation directly to homolog first to lock the ID:
     `bravros commit "📋 plan: reserve $PLAN_ID $SLUG" .planning/P-*`
     `git push origin homolog`
   - Switch back to the feature/worktree branch and merge: `git merge origin/homolog`.
2. **Interview**: Ask only diverging questions. Save closed decisions & canonical constraints in `README.md`.
3. **Write & Review**:
   - Write `README.md` following [`dossier-template.md`](references/dossier-template.md).
   - Review inline (validate path existence, tier markers `[H]/[S]/[O]`, dependencies, and CLI smoke tests).
4. **Record & Handoff**:
   - Append `created` and `reviewed` events to `.planning/events.jsonl`.
   - Commit: `bravros commit "📋 plan: add P-NNNN <slug>" .planning/`.
   - Hand off to `/orchestrate .planning/P-NNNN-<slug>/`.

## Flags
- `--auto`: Skip interactive prompts.
- `--worktree`: Execute within an isolated worktree via [`worktree-extension.md`](references/worktree-extension.md).
