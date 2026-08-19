# Dispatch Guide

## Two Real Primitives

**Default — parallel subagents:** N flat subagent calls in ONE message; all workers are
direct children of the main session. Use this for almost everything.

**Escalation — one real agent team** (`TeamCreate`): only when a round genuinely needs
mid-task steering or inter-worker discussion. Requires
`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`; flag off → fall back to parallel subagents.
One team at a time — "parallel teams" physically becomes one team with more teammates.

**Orchestrator coding rule:** delegate all phase-level work. A trivial surgical edit (a few
lines) is acceptable when dispatching a worker costs more than the fix. Never take on a
whole phase or run the full test suite yourself.

## Model Selection

The tier IS the model — an `Subagent` call dispatching an `[S]` phase MUST set `model: "sonnet"`:

| Phase tier | Model | When |
|---|---|---|
| `[H]` | **Haiku** | CRUD, styling, config, migrations, trivial tests |
| `[S]` | **Sonnet** | Business logic, services, integrations, real tests |
| `[O]` | **Opus** | Architecture, cross-system coordination (rare) |

Orchestrator model = Sonnet by default; Opus only when the plan has genuine
architecture-tier phases.

## Worker Prompt Template

Include in every worker prompt:

```
You are worker-N, implementing ONE phase of plan <P-ID>.
You implement the phase directly — do NOT spawn sub-agents or sub-teams.

## Worker hygiene (canonical — sync with CLAUDE.md § Subagent & worker hygiene)
- Step 0: `pwd && echo "$(git branch --show-current)"`; absolute paths thereafter; on
  mismatch with this prompt, stop and report.
- Read a file (or the relevant range) before its first Edit; re-Read before re-editing if
  any other step or agent may have touched it since.
- Schema-validated return: match the exact JSON shape under RETURN SCHEMA, every
  `required` field included. On validation failure, feed the validator error verbatim
  into exactly ONE corrected retry.
- gh gotcha: `gh pr diff` has NO `--stat` — use `--name-only` or
  `gh pr view --json files,additions,deletions`.
- Long-running commands go to background (`run_in_background` / `--bg`).
- If the repo has `graphify-out/graph.json` or `.graphify`, answer structural questions
  ("who calls X", blast radius) with `graphify query "<question>"` before any broad
  grep sweep; confirm hits with a targeted Read.
- Heartbeat: emit `worker-N: <phase> — <step>` on every phase-step transition and at
  least every ~10 minutes during long steps.

## Project
- Working directory: <abs-path>
- Plan file: <abs-path>
- Your phase: Phase N — <Name>

## Scope
Read only your phase block plus the files in its **Touches:** and **Context** sections —
the plan file is self-sufficient. Implement every task; run the linter and targeted tests
for the files you touched.

## Pre-existing failure floor (HARD RULE)
You do NOT get to decide a failing test is "pre-existing" — report it and let the
orchestrator classify it. A failure counts as pre-existing ONLY if it appears in the
recorded base-branch baseline (`.planning/.verify-suite-pre-fail.json`) or reproduces at
`origin/<base>`. `git stash` on a clean tree is EXPLICITLY INVALID proof — stashing
nothing changes nothing. Report every failure verbatim; never silence or skip/xfail a
test to make a phase look green.

## WHEN YOU FINISH
1. Compute scope: `{ git diff --name-only; git ls-files --others --exclude-standard; } | sort -u`
   Anything outside your **Touches:** → HALT and report.
2. Commit: `bravros commit "<emoji> <type>: <desc>" <file1> <file2> ...`
   NEVER blanket-stage. NEVER add AI signatures.
3. Mark your phase tasks `[x]` with inline `✅ <timestamp>` suffixes (`date "+%Y-%m-%dT%H:%M"`),
   commit separately.
4. Report back: files changed, test results, commit SHA.

## RETURN SCHEMA (dispatcher: fill in when the return is schema-validated; delete otherwise)
Exact JSON shape (keep `required` minimal):
  <inline the exact schema here>
One minimal correct example:
  <inline one minimal valid instance here>

HARD RULES: commit BEFORE reporting; never leave code uncommitted;
never stage .env/credentials; never push.
```

## Opening a PR from a /pr phase

A worker whose phase is labeled `/pr` delegates PR creation to the `/pr` skill (Skill
tool) — it carries the plan-aware title/body logic and branch checks that a bare
`gh pr create` lacks. Before pushing, verify the resolved branch matches the branch you
were dispatched onto — never push a guessed branch. If the `/pr` skill is unavailable,
HALT and report the blocker instead of calling `gh pr create` directly.

## Watchdog Protocol

- **Worker heartbeat:** one-line status on every phase-step transition, at least every
  ~10 minutes during long steps. Long commands go to background so heartbeats keep flowing.
- **Orchestrator watchdog:** a worker silent for 2 consecutive heartbeat windows (~20 min)
  is presumed stalled — poll its output once; still silent → mark the phase failed and
  re-dispatch rather than waiting indefinitely.

## Commit Hygiene

1. Workers pass explicit paths (their diff ∩ **Touches:**) to `bravros commit`.
   Blanket-stage (`git add .`) is forbidden — it causes cross-phase file leaks.
2. After all workers complete, the orchestrator runs `git status --porcelain`. Non-empty
   output stops the round — uncommitted deliverables must resolve before the next round.
3. NEVER add AI signatures. NEVER stage `.env`, `*-api-key`, or credential files.
