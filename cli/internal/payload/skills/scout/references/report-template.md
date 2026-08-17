---
name: debug-report-template
description: Durable report + diagnosis templates for /root-cause skill. Produces diagnosis.md and report.md inside a D-NNNN investigation directory. Frontmatter uses the D-NNNN schema.
---

# Debug Report & Diagnosis Templates

Step 7 of `/root-cause` writes two files into `$DEBUG_DIR` — `diagnosis.md` (the synthesis + proof) and `report.md` (the durable record). Both schemas are here.

## report.md — Frontmatter Schema

~~~yaml
---
id: "D-NNNN"
title: "debug: <short description of the bug investigated>"
type: debug
status: open            # open | in-progress | resolved | escalated | wont-fix
severity: medium        # critical | high | medium | low | info
confidence: high        # high | medium | low  (low ⇒ UNCERTIFIED)
project: <project-name from basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")">
linked_to: ["pending-handoff"]   # Step 7 sets placeholder; receiver updates to B-NNNN | P-NNNN | quick:<branch>
plan: null              # filled when escalated to /recon
backlog: null           # filled when added to backlog
pr: null                # filled when the fix ships
tags: []
root_cause: null        # one-line root cause — prefix "UNCERTIFIED — " if Step 6 round cap was hit
rounds: 1               # number of investigation rounds run (1–3)
certified: true         # true once Step 6 certification passes; false ⇒ UNCERTIFIED
created: <date "+%Y-%m-%dT%H:%M">
resolved: null
---
~~~

## Directory Layout

```
.planning/debug/D-NNNN-<slug>-open/
├── report.md              # durable record (this template)
├── diagnosis.md           # synthesis + Proof of Root Cause (Step 7)
├── agent-1-findings.md     # verification agent — Code Trace
├── agent-2-findings.md     # verification agent — Call-Site & Blast Radius
├── agent-3-findings.md     # verification agent — Reproduce & Runtime Evidence
└── agent-N-findings.md     # N increments across re-investigation rounds (round 2 → 4,5,6 …)
```

The directory is never renamed: closing the investigation is a `completed`/`cancelled` event appended to `.planning/events.jsonl` (see `.planning/CONVENTIONS.md`). The `-open` suffix `nextid reserve` currently produces is a legacy artifact — events outrank suffixes. Additional ad-hoc files (`reproduction.md`, `proof-transcript.txt`) are allowed — all subfiles are committed and kept permanently.

## report.md — Body Template

~~~markdown
## Summary

One paragraph: what was investigated, the scope of the bug, the number of
investigation rounds, and the confidence level.

## Evidence

<!-- key findings copied from agent-N-findings.md -->
- **File:** `path/to/file.ext:NN` — description of evidence
- **Log entry:** timestamp, message, stack trace excerpt
- **Call sites:** how many, isolated vs cross-cutting (from Agent B)

## Root Cause

**File(s):** `path/to/file.ext:NN`
**Issue:** Specific description — wrong condition, missing null check, stale cache, race condition, etc.
**Mechanism:** Why this produces the observed symptom.
**Confidence:** high | medium | low

## Proof of Root Cause

<!-- MANDATORY. A /root-cause report without this section is incomplete. -->
**Proof type:** reproduce+state-match | counterfactual | evidence-chain
**Transcript:** the actual runtime output from Step 6 — paste it, do not paraphrase:

```
<query results / tinker output / test-failure transcript>
```

**Why this certifies the root cause:** one or two sentences tying the transcript
to the hypothesis.

## Blast Radius

- **Severity:** critical | high | medium | low
- **Affected:** what users / features / data are impacted
- **Scope:** isolated to one file/component | cross-cutting | data corruption | production impact

## Recommendations

<!-- choose based on scope; see investigation-guide.md decision matrix -->
- [ ] `/quick` — certified small single-file fix
- [ ] `/backlog` — certified simple fix, schedule later
- [ ] `/recon` — 3+ files or architectural implications
- [ ] Leave as-is — investigation is the permanent record
~~~

### UNCERTIFIED variant

If Step 6 hit the 3-round cap without certifying anything, set `confidence: low`,
`certified: false`, prefix `root_cause` with `UNCERTIFIED — `, and replace the
**Proof of Root Cause** section with:

~~~markdown
## UNCERTIFIED — Hypotheses Tried

No hypothesis could be certified within the 3-round cap.

- **Round 1 hypothesis:** … → refuted by: <evidence>
- **Round 2 hypothesis:** … → refuted by: <evidence>
- **Round 3 hypothesis:** … → refuted by: <evidence>

**Recommendation:** deeper investigation required. Do NOT attempt a fix on the
current evidence — any fix would be a guess.
~~~

## diagnosis.md — Template

~~~markdown
# Bug Diagnosis: {SHORT_DESCRIPTION}

**Investigation:** {DEBUG_ID}
**Investigated:** {date "+%Y-%m-%dT%H:%M"}
**Confidence:** high | medium | low
**Rounds:** {N}
**Agents dispatched:** {total across all rounds}

## Summary
One paragraph: what is broken, why, and where.

## Root Cause
- **File(s):** `path/to/file.ext:NN`
- **Issue:** specific description
- **Mechanism:** why it produces the symptom

## Proof of Root Cause
**Proof type:** reproduce+state-match | counterfactual | evidence-chain
**Transcript:**
```
{actual query results / tinker output / test failure}
```
**Why this certifies it:** one or two sentences.
<!-- UNCERTIFIED ⇒ replace with the "Hypotheses Tried" list from the report -->

## Contributing Factors
- secondary issues — related but not the root cause

## Blast Radius
- **Severity:** critical | high | medium | low
- **Affected:** users / features / data impacted
- **Scope:** isolated | cross-cutting

## Recommended Fix Direction
- what needs to change — intent, not code
- estimated complexity: small | medium | large

## Sentry
- Issue: {URL if found}
- Events: {count and timeframe}
~~~

## Cross-Linking

Use bare IDs — never encode a stage suffix into a link:

- From the report's `linked_to`: `B-NNNN` | `P-NNNN` | `quick:<branch>`
- From the backlog/plan's `debug:` field: `D-NNNN` (bare ID)
- The bidirectional link completes when the receiver rewrites `linked_to` from `pending-handoff` to its real ID.

## Escalate Routing

When the user chooses **Escalate to /recon**:

1. Read this report's `root_cause`, `severity`, **Proof of Root Cause**, and **Where to Fix**.
2. Invoke `/recon` with: **Goal** = fix `<root_cause>`, **Context** = this report body (proof included), **Affected files** = the blast-radius file list.
3. After the plan is created, update this report's `plan:` field and `linked_to`.

When the user chooses **Add to backlog**:

1. Write the `B-NNNN` file directly per `.planning/CONVENTIONS.md` — id via `bravros nextid`, one `created` event appended to `.planning/events.jsonl`; type fix, title from `title` (strip the `debug: ` prefix), body = `root_cause` + severity/blast radius + the proof transcript + link to `$DEBUG_DIR`, frontmatter `debug: D-NNNN`.
2. Update this report's `backlog:` field and `linked_to`.
