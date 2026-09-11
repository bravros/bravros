---
name: orchestrate
description: Plan and run implementation from a .planning findings dossier — you derive units, waves and model tiers for maximum parallelism, subagents write the code, you verify diffs and commit. Use on /orchestrate [folder].
core: true
---

# Orchestrate — plan the execution, then run it

> **CRITICAL RULE**: Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

You are the ORCHESTRATOR. Subagents write the product code; you read, plan, dispatch, verify diffs,
and keep the task list as the single source of truth. Never write product code yourself.

**The dossier documents findings. The execution plan is yours** — `/recon` deliberately does not
write phases, ordering or tiers, because those are decided better with the whole picture in view.

## Core Rules & Workflow

1. **Absorb the dossier.** Resolve the folder in `./.planning/` or the workspace one level up. Read
   every file whatever its format, fold `events.jsonl` (dedupe by `id`, sort by `ts`; events outrank
   filename suffixes). Verify load-bearing premises against the live tree — dossiers go stale, and a
   wrong premise stops you before any dispatch.

2. **Plan the execution — yours, never the dossier's.** From each issue file's `Implicates:` /
   `Tests:` / `Depends on:` / `Kind:` / `Confidence:` header:
   - cut **units of work** (one issue, or several merged when they share implicated files);
   - build the dependency graph — declared `Depends on:` edges plus shared-file edges;
   - cut **waves for maximum parallelism**: a wave is every unit with no shared implicated file and
     all dependencies met. Read-only (`diagnosis-only`, `needs-fact`) and re-verify units go in wave A;
   - decide the **hot-file strategy** explicitly when one file would serialize most units — one
     owner-worker carrying several issues, or a split-first unit at `[O]`;
   - schedule a **re-verify unit** ahead of any fix whose claim is tagged `READ` or `ASSUMED`;
   - assign one tier marker per unit and write it all to `<dossier>/execution-plan.md` as
     `### Phase N: Name [T]` blocks with `**Touches:**`, `**Context:**`, checkbox tasks and
     `**Verify:**` — the grammar `phase-implementer` parses. Track units as tasks.

   A dossier that already carries `### Phase` blocks is a **legacy shape**: reuse the task text,
   re-derive grouping, order and tier yourself.

3. **Branch gate — never orchestrate product code on the staging or main branch.** Before the first
   edit: `pwd && git branch --show-current`; `STAGING=$(bravros config get staging_branch)`;
   `[ "$(bravros config get police.direct_main 2>/dev/null)" = true ] && STAGING=main` (the config
   default is `homolog`, and a direct-main repo has no such remote branch to fetch). If the branch
   is `$STAGING` or `main`/`master` AND this checkout is not a linked worktree
   (`git rev-parse --path-format=absolute --git-dir` equals
   `git rev-parse --path-format=absolute --git-common-dir` — the relative forms disagree from any
   subdirectory of the main checkout and read as "linked"), cut the branch first:
   `git fetch origin "$STAGING" && git checkout -b feature/p-NNNN-<slug> "origin/$STAGING"`
   (`fix/p-NNNN-<slug>` for a defect dossier; slug from the dossier folder). Already on a dedicated
   branch or inside a worktree → stay there, never switch. Only `.planning/` bookkeeping (id
   reservation, events, dossier edits) may be committed on the staging branch. Operator's rule,
   verbatim: "on orchestrate, it should always if not in a dedicated worktree or branch create a new
   branch before orchestrating never orchestrate directly in homolog".

4. **Dispatching**: name every agent; set `model:` explicitly on EVERY dispatch and make it match
   the phase marker (`[H]`→haiku, `[S]`→sonnet, `[O]`→opus). Omitting it does not pick a tier — it
   silently inherits your session model, so phases written `[S]`/`[H]` all run on the orchestrator's
   model. Spawn a whole wave in ONE message. Never two writers on one file. graphify before broad greps.

5. **Per-unit loop**: dispatch → haiku verifier runs ONLY targeted tests → review the diff yourself →
   `bravros commit` → mark done. A correction goes to the SAME agent via SendMessage; resume beats
   respawn. Watchdog: a worker silent >15 min or ~100k tokens → SendMessage for partials; nothing
   useful by your next turn → TaskStop and work from the partials. "Never the full suite" is a
   PHP/Pest rule (the operator's gate, separate tab); Go and other fast suites run without asking.

6. **Acceptance**: after the last wave, dispatch `acceptance-verifier` against the dossier's
   `acceptance.md`. Write the verdict table and the wave plan into `<dossier>/orchestration-log.md`,
   append a `planned` and a `completed` event, then announce:
   ```bash
   bash ~/.agent_config/scripts/announce.sh --force "Plano <NUM> orquestrado, todas as fases concluídas. Ramo <fragmento>, projeto <repo>." studio || true
   ```
