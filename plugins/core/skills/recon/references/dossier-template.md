# Dossier template — what `/recon` writes, what `/orchestrate` reads

One scope = one folder, `.planning/P-NNNN-<slug>/`. Born once, **never renamed**; no
`-todo`/`-approved`/`-complete` suffixes, no mutable frontmatter. State folds from
`.planning/events.jsonl` (`.planning/CONVENTIONS.md` is canonical).

The consumer is `/orchestrate` — its parser is described in
`skills/orchestrate/references/dossier-format.md`. Write the shape below and it needs zero
translation: README is the entry point, phases carry markers, `Touches:` drives parallelism.

## Folder

```
.planning/P-NNNN-short-slug/
├── README.md          # entry point — brief + phases + acceptance. Always present.
├── NN-<topic>.md      # optional: evidence, deep-dives, recon output. Linked from README.
└── decisions.md       # optional: closed questions from the interview
```

Small plans are README-only. Split a sibling file out when a section would bury the phases —
raw command output, long tables, per-repo recon. The README always declares the read order.

## `README.md`

```markdown
---
id: P-0123
title: Short imperative title
type: feat            # feat|fix|chore|refactor|migration|security|docs
repo: bravros         # or [bravros, other] for cross-repo
created: 2026-08-13
---

# {title}

> **Type:** execution scope — reviewed, ready for `/orchestrate`.

{3–8 lines: what, why, the constraints that bite, key file paths. Link provenance —
[B-0042](../backlog/B-0042-slug.md), [D-0007](../debug/D-0007-slug/) — as body links,
never frontmatter.}

## What is canonical and NOT changing
- {contract / wire format / public API the phases must not touch}

## Closed decisions
- {question} → {answer}. Do not reopen.

## Traps
- {a known way to break something while fixing it, and the phase it threatens}

## Phases

### Phase 1: {Descriptive Name} [S]

**Touches:** `cli/internal/plan/`, `cli/cmd/plan.go`

- [ ] Task — one action per checkbox; names its file and its deliverable
- [ ] Create test for X scenario

**Verify:** `go test ./cli/internal/plan/ -run TestX`

### Phase 2: {Descriptive Name} [H]

**Touches:** `docs/CLI.md`

- [ ] …

**Verify:** `…`

## Acceptance
- [ ] Criterion phrased as observable behavior, not "tests pass"
- [ ] All existing related tests pass
<!-- only when a phase Touches: a cli/ path — omit entirely otherwise -->
- [ ] Smoke per `skills/shared/smoke-gate.md`: build to a scratch path, run the affected
      verb, paste observed output
```

## Rules that make it executable

- **One tier marker per phase heading** — `### Phase N: Name [S]`. Never per-task. `[H]`
  mechanical (CRUD, config, renames, docs), `[S]` real reasoning (logic, integrations,
  complex tests), `[O]` architecture / cross-system (rare). **Marker IS the model.**
- **`Touches:` names real paths**, verified to exist. `/orchestrate` groups phases with zero
  `Touches:` overlap into one parallel round — two phases on the same file must be ordered.
- **No phase depends on a later phase's output.** Order encodes dependency.
- **A "verify + fix" phase splits in two** — verify-only, then fix. A worker cannot honestly
  verify its own fixes.
- **No `## Execution Strategy` section** — `/orchestrate` re-derives round grouping itself.
- **Phase 0 = re-verify (read-only)** whenever the plan rests on counts, line numbers, or
  "X is missing" claims that could go stale between writing and execution.
- Timestamps from `date "+%Y-%m-%dT%H:%M"`.

## Superseding

Replacing a dossier → add a `⚠️ Superseded by P-NNNN` banner at the top of the old README and
keep the folder as evidence. Never delete it, never rename it.
