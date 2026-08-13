---
name: root-cause
description: Investigate bugs with parallel subagents, then certify the root cause with runtime proof before handing off. Use on `/root-cause` for read-only diagnosis that routes to /quick, backlog, or /plan.
---

# Root Cause — investigate, verify, certify

Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/root-cause/references/briefing.md) on demand for detailed context and instructions.

INTENT: prove a root cause with runtime evidence, then hand the fix elsewhere. Investigation produces a hypothesis; only certification makes it a diagnosis. Nothing certifiable → say so plainly (`UNCERTIFIED`), never ship a guess.

HARD CONSTRAINTS:
- **NEVER modifies application code.** Writes only inside reserved `$DEBUG_DIR`; fix happens via `/quick`, backlog, or `/plan`.
- **Certification is the gate.** Needs reproduction + state match, counterfactual, or unbroken evidence chain (cookbook: `references/certification.md`). Cap: 3 rounds / 3 parallel agents per round.
- **Laravel probes via `php artisan` one-shots** (`tinker --execute`, `db:table`, `route:list`, `storage/logs/laravel.log`). Boost MCP is optional.
- **graphify is a lead source, never a verdict.** Source code always wins.
- **The hand-off is the operator's decision** — always `ask_question`.

## Flow

1. Materialize engine: `mkdir -p .claude/workflows && cp -f ~/.agent_config/skills/root-cause/scripts/root-cause-investigate.js .claude/workflows/root-cause-investigate.js`
2. Reserve dir: `DEBUG_DIR=$(bravros nextid reserve debug --slug "$SLUG"); DEBUG_ID=$(basename "$DEBUG_DIR" | grep -oE 'D-[0-9]+')`. Scan `.planning/` for prior work.
3. Build candidate lead list (`graphify`/`grep`/`git`/`error`). Categorize bug.
4. Run parallel engine:
   ```
   Workflow({ name: 'root-cause-investigate', args: { debug_dir: DEBUG_DIR, bug: ARGUMENTS, category, stack, repro?, leads, boost, max_rounds: 3 } })
   ```
5. Write `diagnosis.md` + `report.md` in `$DEBUG_DIR` (schemas: `references/report-template.md`). Commit: `bravros commit "🔍 debug: $DEBUG_ID investigation for $SLUG" <files>`.
6. Route via `ask_question` (decision matrix: `references/investigation-guide.md`). Backlog route = write `B-NNNN` per `.planning/CONVENTIONS.md` (`bravros nextid` for ID).

Close investigation by appending `completed`/`cancelled` event to `.planning/events.jsonl`. Use `$ARGUMENTS` as bug description.
