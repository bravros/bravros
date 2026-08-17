# /graphify-this-project Briefing

INTENT: end-to-end graphify setup, **in-project model** — `graphify-out/graph.json` committed to THIS repo (travels via `git pull`), named communities, tracked post-merge refresh hooks, union-merge driver, queried via the **user-scoped `graphify` MCP server** (registered ONCE per machine) with the CLI as backup.

Full step sequence, commands, and templates: **`references/setup-runbook.md`** — read it and execute in order (version pin before extraction; `.gitignore` surgery before any commit).

Two refresh tiers, kept separate: **structure** (free AST rebuild on post-merge to the autocommit branch — hook-driven, never CI; labels carried forward by node identity, pre-push guard refuses a push that drops >50% of remote labels, override `GRAPHIFY_ALLOW_LABEL_LOSS=1`) and **semantics** (on-demand paid pass / external labelling prompt — labels are a snapshot; re-clustering renumbers community ids, so re-run after big refactors).

## Hard constraints

- **ALWAYS confirm with the user before dispatching parallel sub-agent waves** — a 30-worker swarm is a noticeable burst; later waves re-confirm. Hooks/merge-driver install (Step 8) is default-on, no ask.
  <!-- announce-template: "Extração do grafo aguarda sua confirmação antes de disparar agentes paralelos. Projeto {PROJECT}." -->
  ```bash
  bravros ha say --force "Extração do grafo aguarda sua confirmação antes de disparar agentes paralelos. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
  ```
- **NEVER skip the version-pin install** (`scripts/apply-dedup-fix.sh`, runbook Step 3) — mixed graphifyy pins on different machines silently overwrite each other's committed graph and strand it unlabeled.
- **NEVER write a plaintext API key to disk or echo it in chat.**
- **NEVER re-add a blanket `graphify-out/` `.gitignore` line** — remove only that line; the granular scratch block (runbook Step 2) keeps `graph.json` trackable.
- **NEVER write a per-project `.mcp.json`**, redirect to `~/Sites/context`, register a `_global` graph, use upstream `graphify install` / `graphify hook install`, wire a CI refresh, or use the retired `graphify-regenerate` merge driver.
- **`--no-viz` everywhere** — only the searchable `graph.json` is kept; HTML viz on explicit user request only, gitignored.
- **`collate-labels.py` is full-relabel-only** — gap-fills go through `merge-missing-labels.py` (`/graphify-status` owns that loop).
- Don't commit a machine-local hook file in a repo that gitignores it (e.g. paylog).

## Flags

| Flag | Effect |
|---|---|
| `--no-hooks` | Skip refresh-hook install (graph stays fresh only via manual re-run — say so in the summary) |
| `--no-merge-driver` | Skip the `graph.json` union-merge driver only |

Parse by glob-matching the whole arg string (`case " $ARGUMENTS " in *" --no-hooks "*)`) — zsh for-loops don't word-split.

Prerequisites: `uv`, `bravros`, `graphifyy` ≥ the pin in `references/.graphify-version` (with `[mcp]` extra); `op` only for the DeepSeek path. Missing → halt and tell the user.
