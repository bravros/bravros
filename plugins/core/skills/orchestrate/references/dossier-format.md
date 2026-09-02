# Dossier format — shapes you will meet inside a `.planning/P-NNNN-*` folder

A dossier folder is a **self-contained brief for one orchestrator session**. Everything the
session needs is in the folder — but the folder is NOT a rigid contract. These are the
recurring shapes, described so you can parse an unfamiliar dossier fast; when a folder
deviates, read everything and infer.

## The folder

A well-organized one looks like:

```
.planning/P-NNNN-short-slug/
├── README.md          # entry point when present — read first, top to bottom
├── prod-queries.md    # evidence: read-only production queries
├── recon-<repo>.md    # evidence: per-repo reconnaissance
├── NN-<topic>.md      # numbered deep-dives with a declared read order
└── events.jsonl       # append-only state log (newer dossiers — see below)
```

Many won't look like this. There may be no README, mixed formats (`.md`, `.jsonl`, `.sql`,
`.txt`, raw command output), and no consistent naming. Read every file regardless. When a
README declares a read order (`## Read in this order` table, or inline links), honor it —
the ordering encodes dependency, not preference.

## JSONL event logs — where state lives in newer dossiers

Plan state is migrating away from filename suffixes (`-todo`/`-complete`) and mutable
frontmatter toward **append-only JSONL event logs**, one JSON object per line:

```jsonl
{"ts":"2026-08-10T09:41:00Z","id":"e_01J9K8E","kind":"started","subject":"P-0123","branch":"feature/x","by":"agent:plan-approved"}
{"ts":"2026-08-10T11:22:31Z","id":"e_01J9KAF","kind":"phase_done","subject":"P-0123","phase":2,"commit":"a1b2c3d","by":"agent:phase-implementer"}
```

Rules for reading one:

- **Status is a fold of the events**, not a field: dedupe by `id`, sort by `ts`, apply in
  order. Ignore unknown `kind`s (forward compatibility). Skip torn/unparseable lines.
- When an event log and a filename suffix or `status:` frontmatter disagree, **the event
  log wins** — suffixes and frontmatter mirrors are the legacy encoding.
- Identity frontmatter (`id`, `title`, `type`, `repo`, `created`) is write-once; anything
  mutable you'd expect there (status, PR number, counters) lives in events instead.
- A `SHIPPED.md` in the folder, or a terminal event (`completed`/`merged`), marks the
  dossier done — equivalent to the legacy `-complete` suffix.

## The type declaration

Near the top, a blockquote declares what the folder IS. Current dossiers say:

> **Type:** findings dossier — reviewed, ready for `/orchestrate` to plan.

**Every dossier is a findings dossier now.** `/recon` documents; you plan. If you meet an older
folder declaring itself an "execution scope" with `### Phase` blocks, that is a **legacy shape**:
reuse its task text, but re-derive grouping, order and tier yourself — its ordering was written
without the whole picture and is usually the slowest safe schedule.

## Supersession banners — check before anything else

A dossier may open with a `⚠️ Superseded` banner pointing at its replacement ("Do not
implement from this file… run P-NNNN instead"). A superseded dossier is kept as **evidence**,
not scope. If the operator hands you a superseded folder, follow the pointer and confirm.

## Recurring sections and what they demand of you

| Section (typical heading) | What it is | What you do |
|---|---|---|
| `How to run this` | execution constraints: phase ordering, repo boundaries, operator gates | Obey literally — "Phase 0 first", "one branch per child repo", "operator-gated, no bulk writes" are hard rules |
| `What already shipped` / delta table | work that landed AFTER the sources were written | Verify each line in the live tree; **re-implement none of it** |
| Gap table (`G1…Gn` / `C1…Cn`) | the actual remaining scope, one row per gap, with repo + source + why | This is the real backlog; phases usually map onto it |
| `The trap — read before writing Phase N` | a known way to break something while fixing it | Read before dispatching that phase; put the constraint verbatim in the dispatch prompt |
| Phases with `[H]/[S]/[O]` markers + checkboxes | the plan | Marker = model tier for the implementer; checkboxes = task granularity |
| `Out of scope / already decided` | closed questions | Do not reopen them |

## Staleness — the single most important trap

Dossiers snapshot production at write time. Counts, line numbers, and "X is missing" claims
routinely predate a hotfix that landed hours later — P-0285's three sources disagreed on the
stranded-order count (32 / 51 / 62) and ALL were stale. Well-authored dossiers encode the
countermeasure as **Phase 0: re-verify (read-only)** — run it first and write the real
numbers back into the README before touching code. If the dossier has no Phase 0, do the
equivalent yourself: verify every load-bearing count and "missing" claim against the live
tree before implementing.

## Authoring a dossier

Not your job — [`recon`](../../recon/references/dossier-template.md) owns the folder contract, and it
is the single source of truth for what a dossier contains. You write only `execution-plan.md`,
`orchestration-log.md` and `runs/`; never edit recon's numbered siblings.

## The current folder shape, for reference

```
.planning/P-NNNN-<slug>/
├── README.md              # summary + issue index + read order (short by contract)
├── 01-evidence.md         # what each artefact in evidence/ SHOWS
├── evidence/              # arrival-numbered, gaps legal, never renumbered
├── 1N-<issue>.md          # one per issue — header block is your input contract
├── decisions.md           # D1..Dn, append-only, supersession by banner
├── traps.md               # keyed by issue id and file
├── acceptance.md          # observable criteria — what acceptance-verifier judges
├── log.md                 # dated wave log
├── execution-plan.md      # YOURS
├── orchestration-log.md   # YOURS
└── runs/                  # YOURS — read-only unit outputs
```

Read the issue header blocks first: `Kind:`, `Confidence:`, `Implicates:`, `Tests:`, `Depends on:`,
`Falsifier:`. Those five fields are what you plan from. `Confidence: READ` or `ASSUMED` means
schedule a re-verify unit before the fix — the `Falsifier:` line tells you what to check.
