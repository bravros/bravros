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
  ask_question.

Read **every file in the folder, whatever its format** — `.md`, `.jsonl`, `.sql`, `.txt`,
anything — before dispatching. The folder will not always be organized: there may be no
README, no declared read order, and no consistent naming. Infer the entry point yourself;
if a read order is declared, honor it (the ordering encodes dependency). If the folder
contains a JSONL event log, **fold state from the events** (dedupe by `id`, sort by `ts`,
ignore unknown kinds) — events outrank filename suffixes and frontmatter when they
disagree.

## Step 1 — Determine the phases

Always produce an explicit phase plan before any dispatch, whatever shape the folder took:

- Usually the dossier has NO plan — derive the phases yourself from the findings/gap
  table: partition by file ownership and dependency, order so nothing blocks on unfinished
  work, and assign each phase a model tier.
- If the dossier does carry explicit phases (checkboxes, `[H]/[S]/[O]` markers), adopt
  them as written, in the dossier's declared order.
- Either way, track the phases as native tasks (TaskCreate) — the task list is the single
  source of truth for progress from here on.

Then validate before implementing. Dossiers go stale fast — counts, line numbers, and
"missing" features often predate a hotfix that landed after the dossier was written.
**Check the load-bearing premises against the live code; if one is wrong, stop and tell
the operator before writing anything.** A "what already shipped" table means exactly that:
verify each line, re-implement none of it.

## Worktree lock

The operator runs multiple worktrees in parallel. Every edit and commit happens HERE. Run
`pwd && git branch --show-current` before the first edit; on mismatch with what the dossier
or operator named, stop and report. Never touch the parent checkout or any sibling.

## Dispatch

graphify before grep — "how does X work / what touches Y / who calls Z" goes to the graphify
MCP first; grep only for exact strings. Subagents follow the same rule.

Model tiering (dossier phase markers `[H]/[S]/[O]` map directly):

| Tier | Work |
|---|---|
| **opus** | ALL code-writing implementers |
| **sonnet** | test authoring, bounded mechanical edits |
| **haiku** | test runs, lint, greps, verification sweeps |
| **you** | orchestration, diff review, integration decisions |

**The marker IS the model — set `model:` explicitly on EVERY `invoke_subagent()` call**
(`model: "opus"` for an `[O]` phase, `"sonnet"` for `[S]`, `"haiku"` for `[H]`). Omitting
`model:` does not pick a sensible tier — it inherits the orchestrator's own session model,
so every worker silently runs on Fable regardless of the markers you just assigned. This
also overrides any agent-type default (`phase-implementer` etc. carry no tier of their
own). Only the orchestrator itself runs on the session model.

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

Name every agent (`name:` is its address). Independent phases spawn in ONE message so they
run concurrently; dependent phases in sequence. Never two writers on the same files —
partition by ownership.

## Per-phase loop

1. Dispatch the phase to an implementer at its marker's tier.
2. On completion, a haiku verifier runs ONLY targeted tests for the touched files
   (`--filter` / paths). Never the full suite — that is the operator's gate, in a separate
   tab (exception: operator is AFK / autonomous run → background it yourself).
3. Review the diff yourself before accepting. Wrong → SendMessage the SAME agent a
   correction; resume beats respawn because the agent still holds its context.
4. Commit per phase via `bravros commit`, mark the task done.

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
self-contained binary is an ask_question, not an interview.

## Done

Done = all phases implemented, targeted tests green, per-phase commits here, task list
reflecting true state. Hand the operator: branch, commits, files touched, the full-suite
command for a separate tab, and anything deliberately skipped. Then announce:

<!-- announce-template: "Plano {NUM} orquestrado, todas as fases concluídas." -->
```bash
bravros ha say --force "Plano {NUM} orquestrado, todas as fases concluídas. Ramo <fragmento>, projeto <repo>." studio >/dev/null 2>&1 || true
```

Direct CLI call, not the `announce.sh` wrapper: `HASS_TOKEN` now comes from the macOS
keychain via `~/.zshenv`, so the wrapper's 1Password hydration is dead weight here —
0.29s instead of 0.80s. Mute is honored either way (both read `~/.bravros/.mute`).
Redirect stdout: the CLI prints `Sent to studio: …`.
