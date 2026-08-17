# Output contracts

The headings are a **completeness contract, not a length demand**. Merge adjacent sections for narrow questions, but never drop evidence status, alternatives, failure conditions, or next actions. Cap high-priority items at five.

## Pre-build strategy

```md
## Project Approach: <Project Name>

### TL;DR
<Recommended approach and why.>

### Project Frame
<Goal, users, constraints, assumptions, success criteria, evidence status.>

### Evidence Reviewed
<Evidence ledger: local/user evidence, external sources, observed dates, research gaps.
 If community sources were offered, state which were selected, declined, or unavailable.>

### Decision Methodology
<Constraints, decision criteria, and how comparables influenced — or failed to influence — the recommendation.>

### Comparable Projects and References
1. **<Name>** — <URL>; <maintenance/adoption signal>; <why relevant>; <what transfers>; <what should not be copied>.

### Recommended Stack
<Frontend, backend, data, auth, hosting, testing, observability, key libraries.>

### Cost and Vendor Reality
<Pricing/limits checked, unverified assumptions, cost growth, lock-in, lower-cost or self-hosted alternatives.>

### Architecture Direction
<Structure. Mermaid or ASCII diagram when it earns its space.>

### Alternatives Considered
1. **<Option>** — <what you gain, what you give up, what becomes harder later, when it is wrong>.

### Build Plan
1. <First useful vertical slice>
2. <Next slice>
3. <Hardening / deploy / testing step>

### Risks and Unknowns
- <What could change the recommendation.>

### References
- <URL>
```

Vague pre-build request that cleared the intake gate: add an `Intake Summary` before `Project Frame`, or state that intake was skipped because constraints were already sufficient.

## Mid-build or post-build review

```md
## Project Approach Review: <Project Name>

### TL;DR
<Verdict, the single most important course correction, and what to keep.>

### Project Summary
<What it does, who it serves, current stack, architecture shape, maturity.>

### Evidence Reviewed
- Commands run: <short list>
- Files inspected: <most important files>
- External references: <count or "not performed">
- Evidence status: <local repo inspected | description only | GitHub URL only | mixed>
- Inspection scope: <mapped / deeply inspected / sampled / skipped>

### Decision Methodology
<Constraints, criteria, comparable influence, transferable patterns, limits of the recommendation.>

### What Is Working
- <Real strengths only, with evidence.>

### Comparable Projects or Benchmarks
1. **<Name>** — <URL>; <signal>; <why comparable>; <what transfers>; <what should not be copied>.

### Gap Analysis
<Gaps between this project, its stated goals, and credible comparables or ecosystem practice.>

### Recommended Changes
#### High Priority
1. **<Change>** — <why, where, expected impact>
#### Medium Priority
#### Low Priority

### Stack and Architecture Verdict
<Keep, adjust, or reconsider — with tradeoffs and migration cost.>

### Cost and Vendor Reality
<As above.>

### Risks, Assumptions, and Unknowns
- <What could change the verdict.>

### References
- <URL or local file reference>
```

## Tradeoff block

Every primary recommendation carries four beats, blunt: **what you gain** · **what you give up** · **what becomes harder later** (migration, scaling, compliance, collaboration, schema change, local dev) · **when this becomes wrong** (the user/team/usage/pricing/compliance condition that should trigger a different choice).

## Calibration

A weekend prototype, hackathon app, internal tool, student project, OSS library, and production SaaS do not get the same standard. Say which one you are grading against.

## Naming the non-obvious result

When research overturns the default answer, say so out loud rather than presenting the conclusion flat: *"The generic answer here is Next.js + Postgres, but the comparable set says Django with SQLite full-text search fits this solo self-hosted scope better, because …"*
