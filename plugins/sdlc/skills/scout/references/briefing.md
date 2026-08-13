# Briefing — Scout Deep Context & Flow

Read [SKILL.md](../SKILL.md) for the high-level workflow summary.

## Overview & Intent

Scout the codebase with graphify and grep, trace references to pinpoint the bug, and create a findings folder under `.planning/scout/S-NNNN-<slug>/` containing findings, diagrams, and logs, without modifying the codebase. Suggestions and findings are handed off to the orchestrator.

## Hard Constraints

- **NEVER modifies application code.** Writes findings and reports only inside its own reserved `$SCOUT_DIR`. Read-only covers *code*, not runtime *inspection* — tinker reads, `SELECT`s, existing tests are fine.
- **Trace references.** Uses graphify and grep to map and pinpoint the issue.
- **The hand-off is the operator's decision** — always `ask_question`, never assume.

## Detailed Workflow Steps

1. Materialize the engine: `mkdir -p .bravros/workflows && cp -f ~/.bravros/skills/scout/scripts/scout-investigate.js .bravros/workflows/scout-investigate.js`
2. Reserve the dir (never hand-`mkdir` — reservation prevents ID collisions across worktrees):
   ```bash
   SLUG=$(echo "$ARGUMENTS" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/-\+/-/g' | cut -c1-40 | sed 's/-$//')
   SCOUT_DIR=$(bravros nextid reserve scout --slug "$SLUG"); SCOUT_ID=$(basename "$SCOUT_DIR" | grep -oE 'S-[0-9]+')
   ```
   Scan `.planning/` (scout dirs, plans, backlog) for prior work on the same symptom — match by behavior, not id. On a hit, announce and ask: continue vs reference the prior finding.
3. Build a **lead list** (candidate files/symbols, tagged `graphify`/`grep`/`git`/`error`) from error text, stack frames, graphify, grep, git blame. Categorize the bug.
4. Run the parallel-lens engine:
   ```javascript
   Workflow({ name: 'scout-investigate', args: { scout_dir: SCOUT_DIR, bug: ARGUMENTS, category, stack, repro?, leads, boost, max_rounds: 3 } })
   ```
5. Write `diagnosis.md` + `report.md` in `$SCOUT_DIR` (schemas: `references/report-template.md`). Then `bravros commit "🔍 scout: $SCOUT_ID investigation for $SLUG" <files>`.
6. Announce, then route via `ask_question` (decision matrix + handoff payload + receiver contract: `references/investigation-guide.md`). Backlog route = write the `B-NNNN` file per `.planning/CONVENTIONS.md` (`bravros nextid` for the id, one `created` event appended to `.planning/events.jsonl`).
   ```bash
   bravros ha say --force "Scout concluído, aguardando decisão sobre o próximo passo. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
   ```

Closing the investigation is an event: append a `completed` (or `cancelled`) event for `$SCOUT_ID` to `.planning/events.jsonl` per `.planning/CONVENTIONS.md`. All artifacts are durable and committed — no cleanup.

Use `$ARGUMENTS` as the bug description, error message, or failing test name.
