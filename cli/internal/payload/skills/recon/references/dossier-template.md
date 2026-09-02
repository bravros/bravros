# Dossier folder contract — what `/recon` writes

One scope = one folder, `.planning/P-NNNN-<slug>/`. Born once, **never renamed**; no
`-todo`/`-approved`/`-complete` suffixes, no mutable frontmatter. State folds from
`.planning/events.jsonl` (`.planning/CONVENTIONS.md` is canonical).

The consumer is `/orchestrate`, which reads this folder and writes its OWN `execution-plan.md`
beside it. **This folder contains findings, not a plan.** Two different recon runs must produce
comparable folders — the shape below is prescriptive.

## Folder

```
.planning/P-NNNN-<slug>/
├── README.md              # SHORT — summary + index. ≤ ~120 lines. Never the detail store.
├── 01-evidence.md         # index of evidence/: one row per artefact, what it SHOWS
├── evidence/              # NN-<what>.{png,jpg,mp4,log,txt,sql} — arrival order, never renumbered
├── 02-diagnosis.md        # optional — a folded /scout report
├── 1N-<issue-slug>.md     # ONE FILE PER ISSUE (or tightly coupled cluster) — the detail lives here
├── decisions.md           # D1..Dn, append-only; supersede by banner, never overwrite
├── traps.md               # keyed by ISSUE id and FILE — never by phase
├── acceptance.md          # observable criteria, grouped by issue id
└── log.md                 # dated wave log: what arrived, changed, was superseded — newest last
```

Numbering: `01–09` context (evidence index, diagnosis, prior art); `10–89` issues. **Gaps are legal**
— evidence is numbered in arrival order and never renumbered, so nobody "tidies" it and breaks a
citation. `/orchestrate`'s outputs never take an `NN-` prefix; they are `execution-plan.md`,
`orchestration-log.md` and `runs/`.

Small scopes collapse: a single-issue dossier may be `README.md` + one issue file + `evidence/`.
Never collapse the issue file INTO the README — that is the failure this contract exists to prevent.

## `README.md` — summary and index only

```markdown
---
id: P-0123
title: Short imperative title
type: fix             # feat|fix|chore|refactor|migration|security|docs
repo: bravros
created: 2026-08-13
---

# {title}

> **Type:** findings dossier — reviewed, ready for `/orchestrate` to plan.

{≤ 8 lines: what is wrong or wanted, why it matters, the constraint that bites. Written ONCE —
later waves append to `log.md`, not here. Link provenance as body links, never frontmatter.}

## Issues

| # | Surface | Symptom | Confidence | File |
|---|---|---|---|---|
| I-01 | `/miner/{address}` chart | wheel-scroll rescales it | CERTIFIED | [10](10-chart-zoom.md) |

## Read order
{every sibling file, in dependency order — omitting one hides it from the orchestrator}

## What is canonical and NOT changing
- {one-line pointers; the reasoning lives in the issue file that owns it}

## Decisions
{one line each + pointer to decisions.md — never restate them here}

## Open questions
{facts owed by the operator or by production, and which issue each blocks}

## Follow-ups — found during recon, deliberately NOT in scope
{real defects you found and are not fixing, so they are not lost}
```

## Issue file — the handoff contract

Fixed header, same field order every time, then free-form body.

```markdown
# I-03 — Transactions read "Not provided" on the template card

Surface:     /miner/{address} → block template card
Kind:        defect            # defect | change | diagnosis-only | needs-fact
Confidence:  CERTIFIED
Evidence:    evidence/05-template-card.png, evidence/14-stale-template.jpeg
Implicates:  app/Services/CKPoolSimpleApiClient.php, app/Livewire/Miner/Show.php
Tests:       tests/Feature/Livewire/MinerShowTest.php
Depends on:  D1, D5
Falsifier:   A /stats response carrying `transactions` while the card still shows "Not provided".

## Symptom
## Chain  — file:line, end to end
## Cause
## Fix direction — WHAT must become true, never the steps or their order
## Acceptance — observable behaviour for this issue
## What NOT to do
```

Field notes:

- **`Kind:`** — `diagnosis-only` and `needs-fact` mark read-only work. `/orchestrate` schedules those
  first; you only label them.
- **`Implicates:`** — the files a fix will most likely touch. An **estimate with a read-from-code
  basis**, not a lock. `/orchestrate` may widen or narrow it. Do not omit it: it is the one thing the
  orchestrator cannot cheaply derive.
- **`Depends on:`** — semantic dependencies only (a decision, another issue, an owed fact). Never
  positional ("after phase 2") — position is not yours to assign.
- **`Falsifier:`** — mandatory. One sentence naming the observation that would prove the analysis
  wrong. This is the single cheapest defence against a confident wrong premise.

## `decisions.md`

Append-only, `D1..Dn`. Each decision carries:

```
Rests on:    I-08 (READ)
Reviewed by: self | fresh-agent | operator
Status:      LOCKED | SUPERSEDED → D-n
```

**`LOCKED` is not `CERTIFIED`.** A decision records an operator's preference; it inherits the
confidence of the claims beneath it. A decision resting on `READ` gets a fresh-eyes pass before it is
locked. Where a decision corrects something written earlier, **state the correction explicitly rather
than quietly overwriting** — and keep the superseded reasoning when its negative finding still holds.

## Superseding

Replacing a dossier → add a `⚠️ Superseded by P-NNNN` banner at the top of the old README and keep the
folder as evidence. Never delete it, never rename it. The same rule applies inside a folder: a
superseded section keeps its text under a banner.

## What recon must NOT write

No `### Phase N`, no `[H]/[S]/[O]` markers, no `**Touches:**`, no `**Verify:**`, no ordering claims
("ordered after…", "can run in parallel with…"), no `## Execution Strategy`, no re-verify phase.
`/orchestrate` derives all of it — including scheduling a re-verify unit wherever your confidence tag
says `READ` or `ASSUMED`. Timestamps from `date "+%Y-%m-%dT%H:%M"`.
