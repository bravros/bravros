# backlog briefing

INTENT: a parking lot for ideas — lightweight to capture, structured enough to evaluate
later. The backlog never implements; promotion hands off to `/plan`.

## Repo facts (events model — `.planning/CONVENTIONS.md` is canonical)

- Item file: `.planning/backlog/B-NNNN-<slug>.md` — **born once, never renamed**.
  Identity-only frontmatter: `id`, `title`, `type`, `repo`, `created`, plus optional
  `severity:` and `source:` (audit|operator|jarvis|incident). No status/priority/size
  fields — state folds from `.planning/events.jsonl`. Legacy suffixed files
  (`-todo`/`-approved`/`-complete`) stay put, read-compatible.
- ID: `BID=$(bravros nextid reserve backlog)`; `bravros nextid release $BID` on abort.
- Lifecycle = one event append each — add → `created` · promote → `promoted` ·
  done → `completed` · drop → `cancelled` (add a `"reason"` field):

  ```bash
  echo '{"ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","id":"e_'"$(date +%s)_$RANDOM"'","kind":"created","subject":"'"$BID"'","by":"agent:backlog"}' >> .planning/events.jsonl
  ```

- Reads: `python3 scripts/planning-events/fold.py` prints the status table (and rebuilds the
  gitignored `.planning/index.json`); item bodies are plain files — just read them.

## Writes land on the base branch — the ID-collision trap

Parallel worktrees each see only their own tree, so an item written inside a worktree hands
out **colliding IDs** and is easy to lose. Every WRITE flow (add/promote/done/drop) runs
with the **primary checkout on the base branch** as cwd, then commits AND pushes — an
unpushed item is invisible to the other worktrees' ID scan. Reads run anywhere.

```bash
BACKLOG_ROOT=$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")
PRIMARY_BRANCH=$(cd "$BACKLOG_ROOT" && git branch --show-current)
# Primary checkout not on the base branch (homolog here) → STOP and tell the user;
# never write the item onto whatever branch happens to be checked out there.
( cd "$BACKLOG_ROOT" && bravros commit "📋 chore: add B-NNNN <title>" .planning/ && git push origin "$PRIMARY_BRANCH" )
```

## Commands

`/backlog` list · `/backlog <number>` view · `/backlog add <text>` capture ·
`/backlog promote <number|N-M>` hand off to `/plan` · `/backlog done|drop <number>` close ·
`/backlog pending group [auto]` cluster. Free text → route by intent.

## Add

1. Duplicate scan: fold table + titles in `.planning/backlog/`. Likely dupe → announce and
   ask (continue / cancel / link) before writing.
   <!-- announce-template: "Item pendente possivelmente duplicado, aguardando decisão. Ramo {BRANCH}, projeto {PROJECT}." -->
   `bravros ha say --force "Item pendente possivelmente duplicado, aguardando decisão. Ramo <fragmento>, projeto <repo>." studio >/dev/null 2>&1 || true`
2. Infer `type` (+ `severity`/`source` when it's a fix or incident); confirm in one
   `ask_question`. Titles: `<type>: short description` — parentheses, em-dashes, and
   10-word titles produce awkward slugs.
   <!-- announce-template: "Novo item pendente aguardando confirmação. Ramo {BRANCH}, projeto {PROJECT}." -->
   `bravros ha say --force "Novo item pendente aguardando confirmação. Ramo <fragmento>, projeto <repo>." studio >/dev/null 2>&1 || true`
3. Write the file (frontmatter + one-paragraph what/why body), append the `created` event,
   commit + push from `$BACKLOG_ROOT`. Surface the ID and mention `/backlog promote NNNN`.

## Promote

1. Announce, then ask: worktree (`/plan --worktree`) or local (`/plan`)?
   <!-- announce-template: "Item pendente pronto para promoção, aguardando escolha. Ramo {BRANCH}, projeto {PROJECT}." -->
   `bravros ha say --force "Item pendente pronto para promoção, aguardando escolha. Ramo <fragmento>, projeto <repo>." studio >/dev/null 2>&1 || true`
2. Read the item; append a `promoted` event (subject `B-NNNN`); hand off to `/plan` with the
   item as context — `/plan` links it in the plan body. The file does not move.
3. After promote, the full `/plan` → `/orchestrate` pipeline applies — the only exception is
   `/quick` for immediate contained fixes.
4. Batch (`N-M`): skip missing/closed IDs, no prompts mid-loop.

## Group (`/backlog pending group`)

Cluster active items into plan-sized groups (shared domain first, then type affinity; split
anything >15 tasks). Show the proposed table, ask which groups to promote. `auto` variant:
dependency-ordered, capped at 6 groups, output a numbered list pasteable into `/orchestrate`.
Grouping appends no events — promotion stays a separate explicit step.
