---
name: orchestrate
description: Orchestrate implementation from a .planning dossier folder — subagents write the code, the session reads, dispatches by model tier, verifies diffs, and commits per phase. Use on /orchestrate [folder] or "implement from this .planning folder".
core: true
---

# Orchestrate — implement from a dossier folder

> **CRITICAL RULE**: Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/orchestrate/references/briefing.md) on demand for detailed context and instructions.

You are the ORCHESTRATOR. Subagents write the product code; you read, decompose, dispatch, verify diffs, and keep the task list as the single source of truth. Never write product code yourself.

## Core Rules & Workflow

1. **Absorb Dossier**: Resolve folder in `./.planning/` or workspace. Read all files & JSONL events. Verify load-bearing premises against live tree.
2. **Phase Planning**: Partition by file ownership & dependency. Map `[H]/[S]/[O]` phase markers directly to model tiers (`opus` implementers, `sonnet` test authors, `haiku` verifiers). Track via tasks.
3. **Worktree Safety**: Run `pwd && git branch --show-current` to ensure operations stay inside this worktree.
4. **Dispatching**: Always set explicit `model:` parameter in worker dispatch. Use graphify before broad greps.
5. **Per-Phase Execution**: Dispatch phase -> run targeted tests via haiku -> review diff -> commit (`bravros commit`) -> mark done.
6. **Completion**: Run targeted CLI announcement when done:
   ```bash
   bravros ha say --force "Plano {NUM} orquestrado, todas as fases concluídas. Ramo <fragmento>, projeto <repo>." studio >/dev/null 2>&1 || true
   ```
