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

## The type declaration — plan vs recon

Near the top, a blockquote declares what the folder IS:

> **Type:** consolidated execution scope for **one** Fable5 session.

vs.

> **Type:** findings / recon dossier for a follow-up implementation session. **Not** an SDLC plan.

This distinction changes your job:

- **Execution scope** → phases exist; validate premises, then run them.
- **Findings/recon dossier** → no plan exists yet; YOU derive the phases from the gap table
  and findings, track them as native tasks, and confirm the derived plan with the operator
  before dispatching if any premise looks shaky.

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

## Authoring a dossier (for the documenting session, not the implementing one)

When you are the session PRODUCING a dossier for a later orchestrator:

- One folder per scope, `P-NNNN-short-slug/`, README as the entry point and the whole brief.
- Open with the type declaration (execution scope vs findings dossier), date, and scope
  (which repos, and why it lives where it lives — workspace `.planning/` for cross-repo).
- State what is canonical and NOT changing (contracts, wire versions) up front.
- Put forensics and raw evidence in sibling files; the README links them with a read order.
- Include the shipped-delta table if anything landed since investigation started.
- Gap table with one row per remaining gap: what, repo, source, why it still matters.
- Name the traps explicitly ("read before writing Phase N") — a trap you discovered and
  didn't write down WILL be re-triggered by the implementer.
- Phases carry `[H]/[S]/[O]` markers and checkbox tasks; Phase 0 is the re-verify pass.
- Mark decided questions as closed so the next session doesn't relitigate them.
- When a dossier is superseded, add the banner + pointer at the top and keep the file as
  evidence; append `-complete` to the folder/file name only when the work actually shipped.
