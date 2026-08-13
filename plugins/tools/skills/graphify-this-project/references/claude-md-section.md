# claude-md-section.md — template for project CLAUDE.md graphify section
# IN-PROJECT, MCP-first template (user-scoped `graphify` MCP server + CLI backup).
# Graph lives at graphify-out/graph.json.
# Replace <NODES>, <EDGES>, <COMMUNITIES>, <ABS-REPO-PATH>, example labels, god nodes.

## graphify (knowledge graph — query this BEFORE grepping)

This project has a graphify knowledge graph stored **in-project** at
`graphify-out/graph.json` (committed to this repo, so it travels via `git pull`). It holds:
- **<NODES> nodes / <EDGES> edges / <COMMUNITIES> communities** (regenerable via `/graphify-this-project`)
- **Semantic community labels** like `<EXAMPLE-LABEL-1>`, `<EXAMPLE-LABEL-2>`, `<EXAMPLE-LABEL-3>` (in `graphify-out/community-labels.json`)
- **God nodes** (most-connected entities): `<GOD-NODE-1>`, `<GOD-NODE-2>`, `<GOD-NODE-3>` ...

> When invoking this skill, fill in the counts, example labels, and god nodes from actual output. Don't ship with placeholders.

### Hard rules — graph-first (MCP preferred, CLI backup)

- **BEFORE running grep / glob / file-tree-walks for codebase questions, query the graph.** It encodes call relationships, contains, imports, semantic-similar links, and named communities — most "how does X work" / "what touches Y" questions resolve in 1 call instead of 5+ greps.
- **Prefer the `graphify` MCP server** (user-scoped, always registered) — one tool call, and the graph stays loaded across calls; the CLI reloads `graph.json` every time. From a session started outside this repo, add `project_path: "<ABS-REPO-PATH>"` to any MCP call.
- **The graph is in-project** — the CLI default `--graph graphify-out/graph.json` can usually be omitted from the repo root.

### When to call which tool

| Situation | MCP (preferred) | CLI backup |
|---|---|---|
| "How does X work / what's connected to Y" | `mcp_graphify__query_graph {question: "how does auth flow work?"}` | `graphify query "how does auth flow work?"` |
| "Show me the path from X to Y" | `mcp_graphify__shortest_path {source: "OrderService", target: "InvoiceService"}` — fuzzy match; `undirected: true` if no directed path | `graphify path OrderService InvoiceService` (exact labels) |
| "Explain what this file/node does" | `mcp_graphify__get_node {label}` + `get_neighbors {label}` | `graphify explain app/Services/WebhookService.php` |
| "Show me everything in <community>" | `mcp_graphify__get_community {community_id}` | `graphify query "community order-lifecycle"` |
| "Who are the most important entities?" | `mcp_graphify__god_nodes` / `graph_stats` | `graphify query "god nodes"` |

### When to skip the graph

- Need an exact string match (regex/literal in source) → grep
- Question is about runtime behavior, logs, DB state → not in graphify (static analysis only)
- Suspect graph is stale → run `git pull`/`git merge` on the autocommit branch to trigger the post-merge structure refresh, or run the LLM refresh below

### Refresh — two tiers

- **Structure refresh (tracked post-merge hook, free, NO LLM):** the graph is rebuilt on `git merge`/`git pull` on the autocommit branch (default `homolog`, override via `GRAPHIFY_AUTOCOMMIT_BRANCH`) — **not by CI, not on every commit**. The tracked delegator `.githooks/post-merge` execs `scripts/graphify-refresh-hook.sh`, which rebuilds AST structure, re-applies the committed community labels by node identity (`scripts/graphify/apply-labels.py`), strips framework-verb god-nodes (`scripts/graphify/strip-framework-verbs.py`), then **auto-commits + pushes** the refreshed `graphify-out/` so every machine gets it on `git pull`. Requires `git config core.hooksPath .githooks`. The hook suppresses HTML viz — we keep only the searchable `graph.json`.
- **On-demand semantic refresh (DeepSeek, paid):** re-derives semantic edges + community labels and writes **in-project**:
  ```bash
  bash ~/.bravros/skills/graphify-this-project/scripts/extract-deepseek.sh .
  ```
  Then commit `graphify-out/graph.json` + `graphify-out/community-labels.json` + `graphify-out/GRAPH_REPORT.md`. The committed `graph.json` is the **labeled snapshot** — only the DeepSeek pass re-derives labels; re-run it after a big refactor. Gap-fill labels with `graphify label . --missing-only --no-viz` — **always `--no-viz`**: no HTML artifacts, only the searchable `graph.json`.
- **Conflict-free merges:** `graph.json` is union-merged via the `merge=graphify` driver (`.gitattributes` + `git config merge.graphify.driver "graphify merge-driver %O %A %B"`), so parallel branches never leave conflict markers — the post-merge hook owns the rebuild.

### graphifyy version

Pinned machine-wide via `~/.bravros/skills/graphify-this-project/references/.graphify-version`
(currently **0.9.38**), installed as `uv tool install "graphifyy[mcp]==<pin>"` — the `[mcp]`
extra ships the user-scoped `graphify-mcp` server. **Every machine must run the same pin**:
different versions produce structurally different graphs and overwrite each other's committed
`graph.json`. The historical 0.8.1 dedup patch is retired — upstream merged and improved it.
