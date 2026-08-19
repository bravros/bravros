# Autonomous Flow Mode

Autonomous behavior for `/auto-pr` (including `--worktree`) — zero user intervention.
Shared stage sequence: `pipeline.md`.

## Critical Rule: NEVER Use Interactive Questions

Every decision point is handled autonomously. The `--auto` flag on delegated skills
(`/plan`, `/orchestrate`, `/pr`) suppresses their checkpoint prompts too.
Autonomous mode never stops for context either — compact and continue; the pipeline must
complete. The sanctioned stops are: catastrophic stage failure (commit what exists,
create the PR with a failure note, report) and the acceptance-verify escape hatch
(`acceptance-verify.md`).

> **CRITICAL — Subagent continuity (B-0173 fix):** when `/auto-pr` runs as a subagent of
> a parent orchestrator, it MUST complete ALL remaining stages locally in the same
> invocation. STATUS lines (e.g. `STATUS: plan-ready. NEXT: orchestrate`) are
> informational breadcrumbs — NOT terminal actions, and the parent does NOT send
> continuation messages between stages. After every STATUS emit, immediately continue to
> the next stage, through Stage 6 + final report, in one uninterrupted run.

## Stage 3: Autonomous Execution

The coordinator orchestrates via `Subagent` calls and never writes feature code.
"Autonomous" means zero `interactive user questions`, not zero delegation.

- **The marker IS the model:** dispatch each phase at its `[H]`→haiku / `[S]`→sonnet /
  `[O]`→opus tier from `/plan`'s inline review. A phase heading with no tier marker was not
  reviewed — STOP and re-run `/plan` on the dossier first.
- Dispatch independent phases in parallel, all in ONE message
  (`dispatch.md` owns the prompt template).
- **Max 2 fix rounds per phase**, then move on and note the issue in the plan —
  don't get stuck on one phase.

## Stage 4: Quality Sweep + Green Gate

### 4a: Plan Check

`/orchestrate` already verifies each phase's diff against its `Touches:`/task-list
declarations as it dispatches — there is no separate plan-vs-implementation stage to
delegate to here. Re-confirm the dossier's task list is fully checked off before moving on.

### 4b: Quality Sweep (coordinator self-review of the diff)

`git diff $(git merge-base HEAD "${BASE_BRANCH:-main}") HEAD` — dispatch fix agents per
issue category, **max 2 self-review rounds**. Two traps worth naming beyond the obvious:

- **Schema mismatches:** `updateOrCreate()`/`create()` attribute arrays copy-pasted from
  another model routinely carry columns that don't exist in the target table — check each
  key against `$fillable` / the migration.
- **Route registration:** new controllers/endpoints/service providers whose routes were
  never registered, or that conflict with an existing package's paths.

### 4c: Integration Test Sweep (Green Gate)

Run every test file created/modified on the branch, plus the transitive set: tests that
reference the modified classes but weren't themselves touched. In a graphify-enabled
repo, widen the transitive set with `graphify query "what depends on <Class>"` — the
literal grep misses facade/DI/route/event consumers; **union** the graph hits with the
grep, never replace it. Run the sweep in background (`--bg`) and poll.

Fix rounds on failure: 2 targeted rounds, then ONE comprehensive round with all failure
output (round 3 — hard ceiling). Still red → mark the failures as blockers in the plan,
set `MERGE_READY=false`, and proceed to PR creation with a warning (last resort, not the
normal path). All green → continue to 4c-bis; do NOT set `MERGE_READY` yet.

### 4c-bis: Acceptance Verify

A green Green Gate means the tests pass — NOT that the plan's acceptance criteria came
true. A test can exercise the wrong path (P-0180: the test called `ParsePlanHeader(dir)`
while the binary calls `FindPlanFile → ParsePlanHeader`). Dispatch a **fresh**
`acceptance-verifier` agent per `acceptance-verify.md` (dispatch snippet, verdict-JSON
contract, loop). Autonomous specifics:

- Always a fresh agent — never a phase implementer, never the coordinator self-certifying.
- **Max 2 rounds.** Round-1 `rejected` → fix the FAILED criteria only, then a NEW
  verifier. A second `rejected` → `ACCEPTANCE_VERIFIED=false`, no round 3, carry the
  failed criteria into the final report as blockers.
- `unverifiable` (no `## Acceptance` in the plan) → print the LOUD warning block from
  `acceptance-verify.md` and halt as an uncertified completion. Do NOT invent criteria.

### 4d: Set Flags

```
MERGE_READY = (Green Gate passed) AND (Quality Sweep clean) AND (Acceptance Verify accepted)
```

Any false → the PR is marked BLOCKED in the final report.

## Stage 5: Create PR and Merge to Homolog

Delegate to `/pr --auto` (base branch, push, `gh pr create`, URL capture). Then:

- Merge feat → homolog per the canonical recipe in [`merge-flow.md`](merge-flow.md)
  (lock → `gh pr merge --merge` → verify → release).
- **Promotion homolog → main is NOT autonomous.** It goes through `/promote`, whose
  token must be minted outside the session — the pipeline stops at homolog and says so
  in the final report.
- If the PR already exists: find it with `gh pr list` and proceed with it.
- Merge conflicts: STOP and report — never force-merge (`merge-flow.md` step 3).

## Stage 6: Review Loop

Max 3 iterations of trigger → wait → fix.

Pre-check once: does a Claude review workflow exist?

```bash
CLAUDE_ACTION=$(gh api repos/{owner}/{repo}/actions/workflows --jq '.workflows[] | select(.name | test("claude|Claude|CLAUDE")) | .id' 2>/dev/null)
```

Empty → skip the fix loop entirely and go to the final report.

### 1. Trigger review

```bash
PR_NUM=$(gh pr view --json number -q '.number')
gh pr comment "$PR_NUM" --body "@claude review this PR and check if we are able to merge. Analyze the code changes for any issues, security concerns, or improvements needed.

Required: end your review with EXACTLY ONE of these lines, as plain text, alone on its own line, as the final line:

BRAVROS-VERDICT: approved
BRAVROS-VERDICT: changes-requested

Do NOT wrap the line in an HTML comment, code fence, blockquote, or list item — plain visible text only. Emit approved only if you would merge this PR as-is. A finding you consider non-blocking does not prevent approved. Any blocking finding requires changes-requested."
```

The trailing sentinel block is **load-bearing for the autonomous merge gate**:
`bravros pr-review --wait` reads the `BRAVROS-VERDICT:` line as the authoritative verdict
and writes the review stamp from it. It must be plain visible text — the @claude GitHub
Action strips HTML comments from posted reviews, so the old `<!-- bravros-verdict -->`
form never survived an Action run (B-0342). Without the marker the CLI must guess from
prose, and it is deliberately biased to fail closed. Keep both sentinel lines byte-exact.

### 2. Wait for the review

```bash
bravros pr-review --wait "$PR_NUM" --timeout 30m
```

Blocks until the bot posts a review or timeout (exit 124 → skip the fix loop, proceed to
final report).

**Stamping is two-tier (P-0183 G1: the marker is the ONLY verdict input).** On a
**tier-1** verdict — the bot emitted the `BRAVROS-VERDICT:` sentinel (legacy HTML form
also accepted) — the marker is authoritative both ways: `approved` auto-writes
`.planning/.review-stamp-<PR>.json` and the pipeline proceeds; `changes-requested` is a
standing veto **no token can override** — address the feedback and re-request review. On
a **marker-less** review, the prose classifier's guess is report-only and never touches
stamp authority: no stamp is written without the operator. Do NOT retry or re-trigger.
Halt and surface the escape hatch: the operator runs `bravros pr-review unlock` from a
**separate terminal outside Claude Code** (the session cannot mint it — that is the
point), then `bravros pr-review "$PR_NUM" --write-stamp`. See `/pr-review` § "Escape
hatch: the review-stamp token".

> **Note (B-0174):** the `Monitor` tool does NOT block inside subagent (`Agent` tool)
> contexts — it returns immediately, giving control back without polling. Always use the
> CLI `--wait` above.

### 3. On review comments

Fetch (`bravros pr-review $PR_NUM`), categorize, dispatch fix agents per category, push.
All addressed → exit loop; else loop (max 3), then note remaining issues.

## Final Report

Post a PR comment summarizing: plan, phases/tasks, review cycles, what was built, test
status (remind that the full suite is the operator's gate), and merge readiness —
`READY` / `BLOCKED` derived from the Green Gate + Acceptance Verify + review results,
listing every blocker verbatim. If BLOCKED: do not merge until blockers resolve.

## Rules for Autonomous Mode

1. **NEVER use interactive user questions.**
2. **NEVER promote to main** — `/promote` with its out-of-band token is the only path.
3. **NEVER skip tests** — workers run targeted; Quality Sweep + Green Gate verify.
4. **Max 3 review cycles; max 2 fix rounds per phase** — prevent infinite loops.
5. **Commit after every stage** — full git history for recovery.
6. **Orchestrate, never implement** — worker subagents do the coding.
