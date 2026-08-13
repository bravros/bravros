---
name: scout
description: Scout and pinpoint issues using graphify and code references without code modifications. Suggestions are passed to the orchestrator.
---

# Scout — investigate and pinpoint issues

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: Scout codebase with graphify and trace references to pinpoint the bug. It produces a detailed findings dossier folder `.planning/scout/S-NNNN-<slug>/` but does not write code fixes. Suggestions and diagnostics are passed directly to the orchestrator.

HARD CONSTRAINTS:
- **NEVER modifies application code.** Writes findings and reports only inside reserved `$SCOUT_DIR`; implementation is handled by the orchestrator.
- **Trace references.** Uses graphify and grep to map and pinpoint the issue.
- **The hand-off is the operator's decision** — always `ask_question`.

## Flow

1. Materialize engine: `mkdir -p .bravros/workflows && cp -f ~/.bravros/skills/scout/scripts/scout-investigate.js .bravros/workflows/scout-investigate.js`
2. Reserve dir: `SCOUT_DIR=$(bravros nextid reserve scout --slug "$SLUG"); SCOUT_ID=$(basename "$SCOUT_DIR" | grep -oE 'S-[0-9]+')`.
3. Build candidate lead list (`graphify`/`grep`/`git`/`error`).
4. Run parallel engine:
   ```
   Workflow({ name: 'scout-investigate', args: { scout_dir: SCOUT_DIR, bug: ARGUMENTS, category, stack, repro?, leads, boost, max_rounds: 3 } })
   ```
5. Write `diagnosis.md` + `report.md` in `$SCOUT_DIR` (schemas: `references/report-template.md`). Commit: `bravros commit "🔍 scout: $SCOUT_ID investigation for $SLUG" <files>`.
6. Route via `ask_question` (decision matrix: `references/investigation-guide.md`). Backlog route = write `B-NNNN` per `.planning/CONVENTIONS.md` (`bravros nextid` for ID).

Close investigation by appending `completed`/`cancelled` event to `.planning/events.jsonl`. Use `$ARGUMENTS` as bug description.
