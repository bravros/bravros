---
name: scout
description: Investigate a defect with graphify and code references, then certify the root cause with runtime proof. Never modifies code. Runs standalone on `/scout <bug>`, or as the defect arm of `/recon`.
---

# Scout — investigate, verify, certify

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: prove a root cause with runtime evidence, then hand the fix elsewhere. Investigation produces a hypothesis; only certification makes it a diagnosis. Findings land in `.planning/scout/S-NNNN-<slug>/`. Nothing certifiable → say so plainly (`UNCERTIFIED`), never ship a guess.

HARD CONSTRAINTS:
- **NEVER modifies application code.** Writes findings and reports only inside reserved `$SCOUT_DIR`; the fix happens through `/recon` → `/orchestrate` or `/quick`. Read-only covers *code*, not runtime *inspection* — reads, `SELECT`s and existing tests are fine; data writes, migrations and side-effecting jobs are not.
- **Certification is the gate.** A diagnosis needs reproduce + state match, a counterfactual, or an unbroken evidence chain (`references/certification.md`). Cap 3 rounds; then report `UNCERTIFIED` honestly and recommend deeper investigation, not a fix.
- **graphify is a lead source, never a verdict** — a confident graph hit still goes through verification; source code always wins.
- **The hand-off is the operator's decision** — always `ask_question`.
- **Called by `/recon`?** Skip the routing question and return `$SCOUT_DIR` plus the verdict; `/recon` folds `diagnosis.md` into its dossier as `01-diagnosis.md` and carries the certified cause into Traps and Closed decisions.

## Flow

1. Materialize engine: `mkdir -p .bravros/workflows && cp -f ~/.bravros/skills/scout/scripts/scout-investigate.js .bravros/workflows/scout-investigate.js`
2. Reserve dir: `SCOUT_DIR=$(bravros nextid reserve scout --slug "$SLUG"); SCOUT_ID=$(basename "$SCOUT_DIR" | grep -oE 'S-[0-9]+')`.
3. Build candidate lead list (`graphify`/`grep`/`git`/`error`).
4. Run parallel engine:
   ```
   Workflow({ name: 'scout-investigate', args: { scout_dir: SCOUT_DIR, bug: ARGUMENTS, category, stack, repro?, leads, boost, max_rounds: 3 } })
   ```
5. Write `diagnosis.md` + `report.md` in `$SCOUT_DIR` (schemas: `references/report-template.md`). Commit: `bravros commit "🔍 scout: $SCOUT_ID investigation for $SLUG" <files>`.
6. Route via `ask_question` (decision matrix: `references/investigation-guide.md`) — **standalone only**. Backlog route = write `B-NNNN` per `.planning/CONVENTIONS.md` (`bravros nextid` for ID). Invoked by `/recon`: return `$SCOUT_DIR` and the verdict instead, and let `/recon` own the routing.

Close investigation by appending `completed`/`cancelled` event to `.planning/events.jsonl`. Use `$ARGUMENTS` as bug description.
