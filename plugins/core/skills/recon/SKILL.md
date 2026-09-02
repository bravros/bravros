---
name: recon
core: true
description: Investigate a bug report or feature request and document it as ONE multi-file .planning dossier for /orchestrate to plan from. Use on `/recon <problem or feature>`, `/recon --worktree`, or `/recon B-NNNN`.
---

# recon

Read [briefing.md](references/briefing.md) for identity reservation, flags and worktree detail.
Folder contract: [dossier-template.md](references/dossier-template.md) — read it before writing.

INTENT: take whatever the operator has — a symptom, a screenshot, a stack trace, a feature idea —
and produce ONE folder `.planning/P-NNNN-<slug>/` that documents **what is wrong, what is wanted,
and what is true**, in enough detail that `/orchestrate` can plan from it without re-investigating
anything.

> **You do not plan the execution.** No phases, no `Touches:`, no `Verify:`, no tier markers, no
> ordering. Sequencing, grouping, parallelism and model tier are `/orchestrate`'s decisions and it
> makes them better with the whole picture in front of it. Your job is findings; its job is a plan.

## 1 — Classify, then reserve

Decide **defect** (something behaves wrong) or **change** (something new). State which and why in one
line; ask only when genuinely unclear. Then `PLAN_ID=$(bravros nextid reserve plan --slug "$SLUG")`.

Attachments — screenshots, logs, exports, recordings — are evidence, not decoration. Copy each into
`evidence/`, numbered in arrival order, and record in `01-evidence.md` **what it shows**, not what you
conclude. **Never assume the content of something you could not open**; say so, and say what you would
need instead.

## 2 — Gather ground truth before writing a line

- **graphify first when the project has it** (`.graphify` or `graphify-out/graph.json`):
  `graphify query "<question>"`, then open the file it names. The graph is a map, not the territory —
  code wins, and a stale label reads exactly like a fresh one.
- **Defect** → hand the hunt to `/scout`: it certifies a root cause with runtime proof, never edits
  code. Fold its `diagnosis.md` in as `02-diagnosis.md`. `UNCERTIFIED` is a valid outcome — then the
  dossier documents the next investigation, not a fix.
- **Change** → ask only where readings genuinely diverge; a deep multi-round fork goes to
  `/interview-me`. Search `.planning/` for prior art before writing a near-duplicate.
- Findings arrive in waves. Append; never rewrite history. A corrected claim gets a
  `SUPERSEDED →` pointer and keeps its original text — the negative finding is usually worth not
  rediscovering.

## 3 — Write the folder

One file per issue, `README.md` as a **short summary and index** — never the detail store. Full
shape in [dossier-template.md](references/dossier-template.md). Every issue file opens with the
fixed header block; those fields ARE the handoff to `/orchestrate`:

```
Confidence:  CERTIFIED | OBSERVED | READ | ASSUMED | SUPERSEDED → <pointer>
Implicates:  <files the fix will likely touch — an estimate, not a lock>
Tests:       <existing test files covering the area>
Depends on:  <D-n decisions, I-nn issues, or a fact the operator/production owes>
Falsifier:   <one sentence: what observation would prove this analysis wrong>
```

`Implicates:` is the one field `/orchestrate` cannot derive cheaply — without it, it must re-read
every issue body to find the file map, which is the re-investigation this split exists to avoid.

**Confidence is mandatory on every issue and every load-bearing claim.** Same vocabulary as `/scout`:
code reading produces hypotheses, only runtime observation produces proof.

| Tag | Means | Must cite |
|---|---|---|
| `OBSERVED` | seen at runtime — live browser, production log, live DB query, executed command | the artefact, or the pasted command + output |
| `READ` | traced in source, not executed | `file:line` |
| `CERTIFIED` | `READ` and `OBSERVED` agree | both |
| `ASSUMED` | neither — states what would settle it | the settling command or question |
| `SUPERSEDED → <ptr>` | kept for history, no longer load-bearing | the pointer |

Never dress an `ASSUMED` claim as `CERTIFIED`. "Correct by construction" is a hypothesis until
something ran.

## 4 — Review your own output

- Every path named exists. Every sibling file appears in the README's read order.
- Every issue carries a `Falsifier:`. A claim you cannot imagine disproving is one you have not
  tested.
- A wrong load-bearing premise → **STOP** and tell the operator. Never paper over it.
- A decision inherits the confidence of what it rests on. A `LOCKED` decision resting on `READ` gets
  a fresh-eyes pass before you call it settled — self-review does not catch a wrong premise.
- Traps are keyed to an **issue id or a file**, never a phase number — there are no phases, and
  numbering you invent would be re-derived away.
- Acceptance criteria are **observable behaviour**, not "tests pass", grouped by issue.

## 5 — Record, commit, hand off

Append `created` and `reviewed` events to `.planning/events.jsonl` (`by: "agent:recon"`), then
`bravros commit "📋 plan: add P-NNNN <slug>" .planning/`. Give the operator exactly one next step:
`/orchestrate .planning/P-NNNN-<slug>/`.

Announce via `~/.agent_config/scripts/announce.sh --force "<PT-BR, ~20 words, ends with origin>" studio || true`.

## Flags

- `--auto`: skip all prompts (used by `/auto-pr`) — print `STATUS: dossier-ready. NEXT: orchestrate`.
- `--worktree`: work inside an isolated worktree via [`worktree-extension.md`](references/worktree-extension.md).
- `/recon B-NNNN`: read the backlog item as context, link it, and append a `promoted` event.
