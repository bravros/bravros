---
name: recon
core: true
description: Turn a bug report or a feature request into ONE reviewed .planning dossier folder ready for /orchestrate. Use on `/recon <problem or feature>`, `/recon --worktree`, or `/recon B-NNNN` to promote a backlog item.
---

# recon

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: take whatever the operator has — a symptom, a screenshot, a stack trace, or a feature idea — and produce ONE folder `.planning/P-NNNN-<slug>/` that `/orchestrate` executes with zero translation.

## 1 — Classify, then reserve

Decide **defect** (something behaves wrong) or **change** (something new). State which and why in one line; ask only when genuinely unclear. Then `PLAN_ID=$(bravros nextid reserve plan --slug "$SLUG")`.

Attachments — screenshots, logs, exports — are evidence, not decoration. Record each path in the dossier and describe what it shows. **Never assume the content of something you could not open**; say so instead.

## 2 — Gather ground truth before writing a line

- **graphify first when the project has it** (`.graphify` or `graphify-out/graph.json`): `graphify query "<question>"`, then open the file it names. The graph is a map, not the territory — code wins, and a stale label reads exactly like a fresh one.
- **Defect** → hand the hunt to `/scout`: it certifies a root cause with runtime proof, never edits code. Fold its `diagnosis.md` into this dossier as `01-diagnosis.md`, and carry the certified cause into **Traps** and **Closed decisions**. `UNCERTIFIED` is a valid outcome — then the dossier plans the next investigation, not a fix.
- **Change** → ask only where readings genuinely diverge; a deep multi-round fork goes to `/interview-me`. Search `.planning/` for prior art before writing a near-duplicate.

## 3 — Write the dossier, then review it inline

`README.md` per [`dossier-template.md`](references/dossier-template.md): brief, what is canonical and NOT changing, closed decisions, traps, phases, `## Acceptance`. Then review your own output:

- Every path named exists. No phase depends on a later phase's output. Two phases touching one file are ordered, not parallel. A "verify + fix" phase splits into verify-only then fix.
- One tier marker per phase heading — `### Phase N: Name [S]`. **Marker IS the model** (`[H]` mechanical, `[S]` reasoning, `[O]` architecture).
- A wrong load-bearing premise → **STOP** and tell the operator. Never paper over it.
- A `cli/` path in any `Touches:` → `## Acceptance` demands a freshly built scratch binary running the affected verb with output pasted.

## 4 — Record, commit, hand off

Append `created` and `reviewed` events to `.planning/events.jsonl`, then `bravros commit "📋 plan: add P-NNNN <slug>" .planning/`. Give the operator exactly one next step: `/orchestrate .planning/P-NNNN-<slug>/`.

## Flags

- `--auto`: skip all prompts (used by `/auto-pr`). `--worktree`: work inside an isolated worktree via [`worktree-extension.md`](references/worktree-extension.md). `/recon B-NNNN`: read the backlog item as context and link it in the brief.
