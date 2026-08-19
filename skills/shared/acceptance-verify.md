# Acceptance Verify — adversarial criteria gate

Single source of truth for the **Acceptance Verify** stage, consumed by `/orchestrate`
(final stage) and `/auto-pr` (Stage 4c-bis) before a plan is declared finished.

The stage answers one question: **did the plan's `## Acceptance` criteria actually come
true in observed behavior?** Not "do the tests pass" — a green test can exercise the
wrong path (P-0180: the test called `ParsePlanHeader(dir)` while the binary calls
`FindPlanFile → ParsePlanHeader`). Build-and-run mechanics live in
[`smoke-gate.md`](smoke-gate.md); this file owns the dispatch, the contract, and the loop.

## Rule 1 — the verifier is always FRESH

The verifier MUST be a newly dispatched `acceptance-verifier` agent with no prior
context on this plan:

- **NEVER** re-use a phase implementer or any worker that wrote code for this plan — it
  will grade its own homework.
- **NEVER** let the orchestrator self-certify. "I reviewed my own diff and it looks
  right" is the failure mode this stage exists to catch.
- Round 2 (below) gets a **new** verifier, not round 1's agent resumed.

## Dispatch

```
Subagent:
  subagent_type: "acceptance-verifier",
  model: "<set explicitly — inherit the orchestrator's tier>",
  prompt: "
You are the acceptance verifier for plan <P-NNNN>.

## Worker hygiene (canonical — sync with CLAUDE.md § Subagent & worker hygiene)
- Step 0: run `pwd && git branch --show-current`; if either disagrees with the values below, STOP and report. Absolute paths in every tool call thereafter.
- You are READ-ONLY: no Edit/Write, never touch `.planning/`, never mark plan tasks, never spawn sub-agents.
- Long-running commands go to background (`run_in_background` / `--bg`); grep/rg to locate, then read targeted ranges — never whole large files.

## Inputs
- Working directory: <abs-repo-path>
- Plan file: <abs-plan-path>
- Base branch: <base-branch>   # diff scope: git diff $(git merge-base HEAD <base-branch>) HEAD

## Task
Read the plan's `## Acceptance` section. For EVERY criterion, derive an executable check, run it,
and record {criterion, result, command, observed}. Build and run the real artifact per
`skills/shared/smoke-gate.md` (never build to `bin/bravros`). Also enumerate call sites of
every symbol this plan changed and confirm none were missed: in a graphify-enabled repo
(`graphify-out/graph.json` or `.graphify`) start with `graphify query "what calls <Symbol>"`,
then grep to confirm each hit and catch dynamic / string-built references the graph cannot see.

A criterion is `pass` ONLY with a pasted command and its observed output. A green unit test is a
claim, not evidence.

Return ONLY the verdict JSON: {\"verdict\":\"accepted|rejected|unverifiable\",\"criteria\":[…],\"notes\":\"…\"}
"
)
```

The agent's frontmatter is `model: inherit`, so the dispatch tier is what it runs at —
set `model:` explicitly on every call.

## The verdict contract

The agent's final message is this object and nothing else:

```json
{
  "verdict": "accepted|rejected|unverifiable",
  "criteria": [
    {
      "criterion": "<verbatim from ## Acceptance>",
      "result": "pass|fail|unverifiable",
      "command": "<exact command run, or \"\">",
      "observed": "<what it actually printed / exit code>"
    }
  ],
  "notes": "<stale consumers, environment gaps, anything the criteria list doesn't cover>"
}
```

| Verdict | Meaning | Orchestrator does |
|---|---|---|
| `accepted` | Every criterion `pass` (`pass` + `unverifiable` with no `fail` also qualifies — read `notes` for the hole). No stale consumers. | Proceed to completion. |
| `rejected` | **ANY** criterion `fail`, or a stale consumer of a changed symbol survived. | Fix loop below. |
| `unverifiable` | The plan has **no** `## Acceptance` section at all. | Escape hatch below — LOUD, never silent. |

## The fix loop — max 2 rounds, hard ceiling

```
Round 1: dispatch fresh verifier
  ├─ accepted     → done
  ├─ unverifiable → escape hatch (below)
  └─ rejected     → fix the FAILED criteria only, commit, then:

Round 2: dispatch a NEW fresh verifier (never resume round 1's agent)
  ├─ accepted     → done
  └─ rejected     → STOP. No round 3. Surface the failed criteria and HALT.
```

- **Never a third round** — two `rejected` verdicts mean the implementation does not do
  what the plan promised; a third auto-fix is thrashing, not converging.
- **Never self-certify** — the orchestrator may not override a `rejected` verdict with
  its own reading of the diff, nor skip the stage because "the tests are green."
- On the second `rejected`: refuse to emit the completion commit / completion announce /
  merge-ready flag; print the failing criteria with their `command` + `observed` fields
  verbatim; return control to the operator.

## Escape hatch — the plan has no `## Acceptance`

A missing `## Acceptance` section is a planning defect and must never be laundered into
a silent pass. On `verdict: unverifiable`, print this block:

```
╔══════════════════════════════════════════════════════════════════╗
║  ⚠️  ACCEPTANCE VERIFY SKIPPED — PLAN HAS NO ## Acceptance        ║
╠══════════════════════════════════════════════════════════════════╣
║  Plan:  <plan-file>                                              ║
║  This plan declares NO acceptance criteria, so nothing could be  ║
║  verified against observed behavior. Completion is NOT certified.║
║                                                                  ║
║  Fix the plan (add ## Acceptance) and re-run, or explicitly      ║
║  acknowledge shipping unverified work.                           ║
╚══════════════════════════════════════════════════════════════════╝
```

- **Interactive mode:** `interactive user questions` — "Ship without acceptance verification?" /
  "Stop and add `## Acceptance`". No silent default.
- **Autonomous mode** (`.planning/.auto-*-lock` present): a sanctioned stop condition —
  halt, print the block, surface it in the final report as an uncertified completion.
  Do NOT invent criteria and verify against them.

`unverifiable` is never reported as success.

## Opt-in: the workflow variant (P-0183 G7)

When `.bravros.yml` has `features.extra.acceptance_verify_workflow: true`, run the
verification as an ultracode Workflow instead of a single agent — same inputs, **same
verdict-JSON contract**. Default (key absent/false) is the single fresh agent above.

What it buys: one verifier per criterion in parallel, plus a **skeptic pass on every
`fail`** — the skeptic tries to REFUTE the fail (wrong fixture, misused verb, misread
output); an unrefuted fail stands; a refuted fail gets exactly one fresh re-run, and a
second fail stands regardless. **The asymmetry is deliberate and fail-closed:** skeptics
can only remove false *vetoes*; passes are never softened (same asymmetry as the
pr-review marker gate).

```bash
mkdir -p .claude/workflows
cp ~/.config/bravros/skills/shared/scripts/acceptance-verify.js .claude/workflows/acceptance-verify.js
```

```
Workflow(
  scriptPath: ".claude/workflows/acceptance-verify.js",
  args: { planFile: "<abs-plan-path>", workdir: "<abs-repo-path>", base: "<base-branch>" }
)
```

The returned object is the verdict contract verbatim — feed it to the same max-2-round
loop and escape hatch. The workflow is read-only (agents never commit/stamp/touch
`.planning/`); mutation stays in the calling skill.

## See also

- [`smoke-gate.md`](smoke-gate.md) — build-and-run mechanics, `suspicious` semantics
- [`dispatch.md`](dispatch.md) — worker prompt template, model selection
- `agents/acceptance-verifier.md` — the agent's own system prompt
