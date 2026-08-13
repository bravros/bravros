# Briefing — Root Cause Deep Context & Flow

Read [SKILL.md](../SKILL.md) for the high-level workflow summary.

## Overview & Intent

Prove a root cause with runtime evidence, then hand the fix elsewhere. Investigation produces a hypothesis; only certification makes it a diagnosis. Nothing certifiable → say so plainly (`UNCERTIFIED`), never ship a guess.

## Hard Constraints

- **NEVER modifies application code.** Writes only inside its own reserved `$DEBUG_DIR`; the fix happens through `/quick`, backlog, or `/plan`. Read-only covers *code*, not runtime *inspection* — tinker reads, `SELECT`s, existing tests are fine; data writes, migrations, shared-cache clears, side-effecting job dispatch are not.
- **Certification is the gate.** A diagnosis needs one of three proofs — reproduce + state match, counterfactual, or unbroken evidence chain (cookbook: `references/certification.md`). Cap: 3 rounds / 3 parallel agents per round, then report `UNCERTIFIED` honestly and recommend deeper investigation, not a fix.
- **Laravel probes via `php artisan` one-shots** (`tinker --execute`, `db:table`, `route:list`, `storage/logs/laravel.log`). Boost MCP is optional — its tool schemas cost tokens artisan doesn't; never require or install it. `php artisan --version` broken at repo root → STOP, that IS the incident.
- **graphify is a lead source, never a verdict** — a confident graph hit still goes through verification; source code always wins.
- **The hand-off is the operator's decision** — always `ask_question`, never assume.

## Detailed Workflow Steps

1. Materialize the engine: `mkdir -p .claude/workflows && cp -f ~/.bravros/skills/root-cause/scripts/root-cause-investigate.js .claude/workflows/root-cause-investigate.js`
2. Reserve the dir (never hand-`mkdir` — reservation prevents ID collisions across worktrees):
   ```bash
   SLUG=$(echo "$ARGUMENTS" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/-\+/-/g' | cut -c1-40 | sed 's/-$//')
   DEBUG_DIR=$(bravros nextid reserve debug --slug "$SLUG"); DEBUG_ID=$(basename "$DEBUG_DIR" | grep -oE 'D-[0-9]+')
   ```
   Scan `.planning/` (debug dirs, plans, backlog) for prior work on the same symptom — match by behavior, not id. On a hit, announce and ask: continue vs reference the prior finding.
   <!-- announce-template: "Possível investigação anterior encontrada. Aguardando sua decisão. Projeto {PROJECT}." -->
3. Build a **lead list** (candidate files/symbols, tagged `graphify`/`grep`/`git`/`error`) from error text, stack frames, graphify, grep, git blame. Categorize the bug: test-failure · runtime-error · unexpected-behavior · performance · integration.
4. Run the parallel-lens engine (arg shapes documented in the script header; `repro` only when a reproduction path exists — its presence dispatches the repro-verifier lens):
   ```javascript
   Workflow({ name: 'root-cause-investigate', args: { debug_dir: DEBUG_DIR, bug: ARGUMENTS, category, stack, repro?, leads, boost, max_rounds: 3 } })
   ```
   Each round: parallel lens agents (code-tracer, blast-radius-mapper, repro-verifier) verify leads against real source → one falsifiable hypothesis → an adversarial agent certifies or refutes it with runtime evidence. The parallel fan-out exists because one agent anchors on its first plausible cause; independent lenses are what surface the refutation. Returns `{ rounds, certified, root_cause, confidence, hypothesis, certification, findings_files, notes }`.
5. Write `diagnosis.md` + `report.md` in `$DEBUG_DIR` (schemas: `references/report-template.md`; the **Proof of Root Cause** transcript is mandatory — paste real runtime output, or the refutation trail if `UNCERTIFIED`). Then `bravros commit "🔍 debug: $DEBUG_ID investigation for $SLUG" <files>`; `DEBUG_COMMIT=$(git rev-parse HEAD)`.
6. Announce, then route via `ask_question` (decision matrix + handoff payload + receiver contract: `references/investigation-guide.md`). Backlog route = write the `B-NNNN` file per `.planning/CONVENTIONS.md` (`bravros nextid` for the id, one `created` event appended to `.planning/events.jsonl`) carrying root cause + proof + `debug: $DEBUG_ID`. `UNCERTIFIED` → recommend backlog-for-deeper-investigation; surface `/quick` last or not at all.
   <!-- announce-template: "Investigação concluída, aguardando decisão sobre o próximo passo. Projeto {PROJECT}." -->
   ```bash
   bravros ha say --force "Investigação concluída, aguardando decisão sobre o próximo passo. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
   ```

Closing the investigation is an event, never a rename: append a `completed` (or `cancelled`) event for `$DEBUG_ID` to `.planning/events.jsonl` per `.planning/CONVENTIONS.md`. The dir keeps whatever name `nextid` gave it (currently `…-open/` — a legacy suffix; events outrank suffixes). All artifacts are durable and committed — no cleanup.

Use `$ARGUMENTS` as the bug description, error message, or failing test name.
