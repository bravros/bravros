# Orchestrate — implement from a dossier folder

You are the ORCHESTRATOR. Subagents write the product code; you read, decompose, dispatch,
verify diffs, and keep the task list as the single source of truth. Never drop down to
writing product code yourself — the moment you open an editor on app code, you've lost the
thread that makes parallel dispatch safe.

Dossier anatomy, staleness traps, and the recon-vs-plan distinction:
[`references/dossier-format.md`](dossier-format.md) — read it before parsing an
unfamiliar folder.

## Step 0 — Resolve and absorb the folder

- Argument given → that folder is the brief.
- No argument → list candidate dossier folders in `./.planning/` (and the workspace
  `.planning/` one level up, if this is a workspace child repo), skipping ones already
  marked done (`-complete` suffix or a `SHIPPED.md` inside), then ask which one via
  `ask_question`.

Read **every file in the folder, whatever its format** — `.md`, `.jsonl`, `.sql`, `.txt`,
anything — before dispatching. The folder will not always be organized: there may be no
README, no declared read order, and no consistent naming. Infer the entry point yourself;
if a read order is declared, honor it (the ordering encodes dependency). If the folder
contains a JSONL event log, **fold state from the events** (dedupe by `id`, sort by `ts`,
ignore unknown kinds) — events outrank filename suffixes and frontmatter when they
disagree.

## Step 1 — Plan the execution (this is your job, not the dossier's)

`/recon` documents findings and deliberately writes **no** phases, ordering, `Touches:` or tier
markers. You derive all of it, with the whole picture in view. Produce an explicit plan before any
dispatch.

### The inputs recon owes you

Each issue file opens with a fixed header:

```
Kind:        defect | change | diagnosis-only | needs-fact
Confidence:  CERTIFIED | OBSERVED | READ | ASSUMED | SUPERSEDED → <pointer>
Implicates:  <files the fix will likely touch — an ESTIMATE, not a lock>
Tests:       <existing test files covering the area>
Depends on:  <D-n decisions, I-nn issues, or a fact the operator/production owes>
Falsifier:   <what observation would prove the analysis wrong>
```

`Implicates:` is an estimate with a read-from-code basis. **You may widen or narrow it** — what you
grant a worker in `Touches:` is the real lock, and `phase-implementer` HALTs on any diff outside it.

### The algorithm

1. **Units.** One issue, or several merged when they share implicated files. A "verify + fix" pair is
   always two units — a worker cannot honestly verify its own fix.
2. **Graph.** Edges from declared `Depends on:` **plus** shared-file edges (two units implicating one
   file cannot run together).
3. **Waves.** A wave is the maximal set of units with no shared implicated file and every dependency
   met. Read-only units (`diagnosis-only`, `needs-fact`) and re-verify units go in wave A — they
   unblock everything and touch nothing.
4. **Re-verify.** Any fix resting on a claim tagged `READ` or `ASSUMED` gets a read-only unit ahead
   of it that settles the claim (the issue's `Falsifier:` tells you what to check). `CERTIFIED` and
   `OBSERVED` dispatch directly. This replaces the recon-authored "Phase 0".
5. **Hot files.** When one file is implicated by most units, a naive graph serializes everything.
   Decide explicitly and record why: one owner-worker carrying several issues through the file, or a
   split-first unit at `[O]` that makes later waves parallel. Do not default to the long serial chain.
6. **Tier per unit**, by size and kind — `[H]` mechanical (CRUD, config, renames, styling, docs),
   `[S]` real reasoning (logic, integrations, complex tests), `[O]` architecture or cross-system.
7. **Write `<dossier>/execution-plan.md`** — `### Phase N: Name [T]` blocks, each with
   `**Touches:**` (the real lock), `**Context:**` (files to read but not edit), checkbox tasks, and
   `**Verify:**` built from the issue's `Tests:`. A `cli/` path in any `Touches:` → the acceptance
   criterion demands a freshly built scratch binary running the affected verb with output pasted.
8. **Track units as tasks.** The task list is the single source of truth for progress from here on.

Then validate before implementing. Dossiers go stale fast — counts, line numbers and "missing"
features often predate a hotfix that landed after the dossier was written. **Check the load-bearing
premises against the live code; if one is wrong, stop and tell the operator before writing
anything.** A "what already shipped" table means exactly that: verify each line, re-implement none.

## Worktree lock

The operator runs multiple worktrees in parallel. Every edit and commit happens HERE. Run
`pwd && git branch --show-current` before the first edit; on mismatch with what the dossier
or operator named, stop and report. Never touch the parent checkout or any sibling.

## Dispatch

graphify before grep — "how does X work / what touches Y / who calls Z" goes to the graphify
MCP first; grep only for exact strings. Subagents follow the same rule.

Model tiering. **The marker IS the model** — you assign the marker per unit in Step 1, then set
`model:` from it on dispatch. Selection criteria:

| Marker → model | The unit is… |
|---|---|
| `[H]` → **haiku** | mechanical: CRUD, config, renames, styling, docs, greps, verification sweeps |
| `[S]` → **sonnet** | real reasoning: logic, integrations, non-trivial tests |
| `[O]` → **opus** | architecture or cross-system: new seams, refactors that move contracts |
| — → **you** | orchestration, diff review, integration decisions. Never product code. |

Targeted test runs after a unit always go to a **haiku** verifier regardless of the unit's own tier.

**Set `model:` explicitly on EVERY `Agent` dispatch.** Omitting it does not pick a sensible tier —
it inherits the orchestrator's own session model, so every worker silently runs on your model
regardless of the markers you just assigned. This also overrides any agent-type default
(`phase-implementer` etc. carry no tier of their own). Only the orchestrator runs on the session model.

Every dispatch prompt carries, verbatim in spirit:

- a **bounded scope** — named files/paths, never "the codebase";
- a **concrete deliverable** — the exact shape to return (`schema` when it matters);
- a **stop rule** — "past ~40 tool calls or nothing new twice running → stop and return
  what you have";
- "Step 0: `pwd && git branch --show-current`; absolute paths inside this worktree only;
  on mismatch stop and report";
- "Read before Edit, always; re-read before re-editing if another step may have touched
  the file";
- "graphify before broad greps".

Name every agent (`name:` is its address). **A wave is the unit of concurrency**: spawn every unit
in a wave in ONE message so they run in parallel, and start the next wave only when the current
one's units are verified and committed — or, for rolling waves, as soon as a unit's dependants are
all satisfied. Never two writers on the same files: partition by ownership.

## Per-phase loop

1. Dispatch the phase to an implementer at its marker's tier.
2. On completion, a haiku verifier runs ONLY targeted tests for the touched files
   (`--filter` / paths). Never the full suite — that is the operator's gate, in a separate
   tab (exception: operator is AFK / autonomous run → background it yourself).
3. Review the diff yourself before accepting. Wrong → SendMessage the SAME agent a
   correction; resume beats respawn because the agent still holds its context.
4. Commit per phase via `bravros commit`, mark the task done.

## Acceptance — the final stage

After the last wave, dispatch the **`acceptance-verifier`** agent against the dossier's
`acceptance.md`. It builds the real artifact, runs the real entry point, and greps for missed
consumers; it never edits files. Give it the criteria verbatim — it must judge OBSERVED behaviour,
not read your commits and agree with them.

## What you write back into the dossier

Recon owns the findings files; you own these, and never edit its numbered siblings:

- **`execution-plan.md`** — the phase blocks you derived (`### Phase N: Name [T]`, `**Touches:**`,
  `**Context:**`, tasks, `**Verify:**`).
- **`orchestration-log.md`** — branch and base, the wave plan and why you cut it that way (especially
  the hot-file decision), a phase status table with commits, the acceptance verdict, and any input
  still owed by the operator.
- **`runs/<unit>-findings.md`** — output of read-only units.
- Events: append `planned` when the execution plan is written, `completed` when acceptance passes.

## Watchdog & hygiene

- Silent ~15 min / ~100k tokens → SendMessage for partials; nothing useful by next turn →
  TaskStop and work from the partials.
- Until a worker's completion notification lands, its result does not exist — never invent
  or summarize what it "probably found".
- TaskStop each worker the moment its output is verified; sweep ListAgents before handing
  back — anything still listed is a leak.

## Decision points

When the next step forks across two+ viable approaches: write findings + your
recommendation, fire the Alexa ping, then run `/interview-me` to lock the branches. A single
self-contained binary is an `ask_question`, not an interview.

## Done

Done = all phases implemented, targeted tests green, per-phase commits here, task list
reflecting true state. Hand the operator: branch, commits, files touched, the full-suite
command for a separate tab, and anything deliberately skipped. Then announce:

<!-- announce-template: "Plano {NUM} orquestrado, todas as fases concluídas." -->
```bash
bash ~/.agent_config/scripts/announce.sh --force "Plano <NUM> orquestrado, todas as fases concluídas. Ramo <fragmento>, projeto <repo>." studio || true
```

Always the wrapper, never bare `bravros ha say` — the wrapper adds the roaming local-`say` fallback
and home/away detection the CLI lacks, and silences its own stdout. Both honour the same kill-switch,
`~/.agent_config/.mute`. One sentence, Brazilian Portuguese, ~20 words, ending with its origin.
