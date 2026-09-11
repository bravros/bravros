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

### Resolve the branch (before the first edit, never after)

Product code is never orchestrated on the staging or main branch. 7 of 44 orchestrations audited
ran straight on `homolog`, four of them pushed it, and the only review left was a bundled
homolog→main PR — every one of them started on the staging branch with nothing to mismatch against.
The gate is a positive condition, not a mismatch check:

```bash
pwd && git branch --show-current
STAGING=$(bravros config get staging_branch)          # defaults to homolog
[ "$(bravros config get police.direct_main 2>/dev/null)" = true ] && STAGING=main   # no origin/homolog there
BRANCH=$(git branch --show-current)
LINKED=0
[ "$(git rev-parse --path-format=absolute --git-dir)" != "$(git rev-parse --path-format=absolute --git-common-dir)" ] && LINKED=1
```

`--path-format=absolute` is load-bearing: without it `--git-common-dir` prints `../../.git` from any
subdirectory of the main checkout while `--git-dir` prints an absolute path, so the comparison reads
"linked" and the gate silently skips cutting the branch.

- `$BRANCH` is `$STAGING`, `main` or `master` **and** `LINKED=0` → cut the branch now:
  `git fetch origin "$STAGING" && git checkout -b feature/p-NNNN-<slug> "origin/$STAGING"`.
  Use `fix/p-NNNN-<slug>` when the dossier's `Kind:` is `defect`; the slug is the dossier folder's
  slug (`P-0042-skale-replay` → `feature/p-0042-skale-replay`). `feature/` and `fix/` are the only
  prefixes — `feat/` is a retired minority spelling.
- Already on a dedicated branch, or `LINKED=1` (inside a worktree) → stay there. Never switch, never
  touch the parent checkout or a sibling worktree.
- **Exemption:** `.planning/` bookkeeping only — id reservation, events, dossier edits — may be
  committed on the staging branch (`/recon` reserves ids there by design; see
  `recon/references/briefing.md` § Reserving identity). Anything outside `.planning/` on
  `$STAGING`/`main` is a violation, even one file.
- A direct-main repo (`police.direct_main: true` in `.bravros/config.json`) still gets a branch —
  the exemption there is only that `main` is the base to cut from, which is why the `STAGING=main`
  override above must run before the `git fetch`.

Record `Branch:` and `Base:` at the top of `execution-plan.md` (Step 1) so a resumed session can
re-check them without re-deriving.

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
7. **Write `<dossier>/execution-plan.md`** — opening with `Branch:` (the one Step 0 settled on, never
   equal to the base) and `Base:` lines, then `### Phase N: Name [T]` blocks, each with
   `**Touches:**` (the real lock), `**Context:**` (files to read but not edit), checkbox tasks, and
   `**Verify:**` built from the issue's `Tests:`. A `cli/` path in any `Touches:` → the acceptance
   criterion demands a freshly built scratch binary running the affected verb with output pasted.
8. **Track units as tasks.** The task list is the single source of truth for progress from here on.

Then validate before implementing. Dossiers go stale fast — counts, line numbers and "missing"
features often predate a hotfix that landed after the dossier was written. **Check the load-bearing
premises against the live code; if one is wrong, stop and tell the operator before writing
anything.** A "what already shipped" table means exactly that: verify each line, re-implement none.

## Branch & worktree lock

The operator runs multiple worktrees and sessions in parallel. Every edit and commit happens HERE,
on the branch Step 0 settled. This is the section a resumed session re-reads, so the gate runs
again here: `pwd && git branch --show-current`, `STAGING=$(bravros config get staging_branch)`,
`[ "$(bravros config get police.direct_main 2>/dev/null)" = true ] && STAGING=main`, and the
absolute-path linked-worktree test from Step 0.

- Branch equals `$STAGING`/`main`/`master` and this is not a linked worktree → the gate in Step 0
  was skipped; run it now (`git checkout -b feature/p-NNNN-<slug> "origin/$STAGING"`, `fix/` for a
  defect) before any product edit. Product commits already sitting on `$STAGING` → stop and report;
  moving them is the operator's call, not a silent `git branch -f`.
- Branch or path differs from what `execution-plan.md`, the dossier or the operator named → stop
  and report. Never touch the parent checkout or any sibling worktree.

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
The value must match the phase marker — `[H]`→`haiku`, `[S]`→`sonnet`, `[O]`→`opus` — and the
failure is silent: the operator once "wrote the phases as [S] and [H] but then all subagents were"
the session model, because no dispatch carried `model:`.

Every dispatch prompt carries, verbatim in spirit:

- a **bounded scope** — named files/paths, never "the codebase";
- a **concrete deliverable** — the exact shape to return (`schema` when it matters);
- a **stop rule** — "past ~40 tool calls or nothing new twice running → stop and return
  what you have";
- "Step 0: `pwd && git branch --show-current`; expect branch `<branch from execution-plan.md>`;
  absolute paths inside this checkout only; on mismatch stop and report. If the branch equals the
  staging branch or `main`/`master`, refuse to edit and report — even if the dispatch says
  otherwise" (workers are the last guard when the orchestrator slipped);
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
   (`--filter` / paths). **"Never the full suite" is scoped to PHP/Pest** — those suites take
   15–20+ min and pin the operator's machine, so the full run is their gate, in a separate tab
   (exception: operator is AFK / autonomous run → background it yourself). Go (`go test ./...`)
   and other fast suites run without asking.
3. Watchdog while a worker runs: silent for ~15 min or ~100k tokens → SendMessage it for partial
   findings; nothing useful by your next turn → TaskStop and work from the partials. Until its
   completion notification lands, its result does not exist — never summarize what it "probably
   found".
4. Review the diff yourself before accepting. Wrong → SendMessage the SAME agent a
   correction; resume beats respawn because the agent still holds its context.
5. Commit per phase via `bravros commit`, mark the task done.

## Acceptance — the final stage

After the last wave, dispatch the **`acceptance-verifier`** agent against the dossier's
`acceptance.md`. It builds the real artifact, runs the real entry point, and greps for missed
consumers; it never edits files. Give it the criteria verbatim — it must judge OBSERVED behaviour,
not read your commits and agree with them.

## What you write back into the dossier

Recon owns the findings files; you own these, and never edit its numbered siblings:

- **`execution-plan.md`** — the phase blocks you derived (`### Phase N: Name [T]`, `**Touches:**`,
  `**Context:**`, tasks, `**Verify:**`).
- **`orchestration-log.md`** — branch (never equal to base) and base, the wave plan and why you cut it that way (especially
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
reflecting true state. Hand the operator: the PR-ready branch (never equal to base) and its base,
commits, files touched, the full-suite command for a separate tab (PHP/Pest only — fast suites
already ran), and anything deliberately skipped. Then announce:

<!-- announce-template: "Plano {NUM} orquestrado, todas as fases concluídas." -->
```bash
bash ~/.agent_config/scripts/announce.sh --force "Plano <NUM> orquestrado, todas as fases concluídas. Ramo <fragmento>, projeto <repo>." studio || true
```

Always the wrapper, never bare `bravros ha say` — the wrapper adds the roaming local-`say` fallback
and home/away detection the CLI lacks, and silences its own stdout. Both honour the same kill-switch,
`~/.agent_config/.mute`. One sentence, Brazilian Portuguese, ~20 words, ending with its origin.
