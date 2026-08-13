---
name: graphify-this-project
description: Set up a graphify knowledge graph for the current project — in-project committed graph.json, AST extraction + external labelling, tracked refresh hooks, union-merge driver, user-scoped MCP query surface. Use on `/graphify-this-project`.
trigger: /graphify-this-project
---

# /graphify-this-project

> Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/graphify-this-project/references/briefing.md) on demand for detailed context and instructions.

INTENT: end-to-end graphify setup, **in-project model** — `graphify-out/graph.json` committed to THIS repo (travels via `git pull`), named communities, tracked post-merge refresh hooks, union-merge driver, queried via the **user-scoped `graphify` MCP server** (registered ONCE per machine) with the CLI as backup.

Full step sequence, commands, and templates: **`references/setup-runbook.md`** — read it and execute in order (version pin before extraction; `.gitignore` surgery before any commit).

## Hard Constraints

- **ALWAYS confirm with the user before dispatching parallel sub-agent waves.**
- **NEVER skip the version-pin install** (`scripts/apply-dedup-fix.sh`, runbook Step 3).
- **NEVER write a plaintext API key to disk or echo it in chat.**
- **NEVER re-add a blanket `graphify-out/` `.gitignore` line.**
- **NEVER write a per-project `.mcp.json`**, redirect to `~/Sites/context`, or register `_global`.
- **`--no-viz` everywhere** — HTML viz on explicit user request only.

## Flags

| Flag | Effect |
|---|---|
| `--no-hooks` | Skip refresh-hook install |
| `--no-merge-driver` | Skip the `graph.json` union-merge driver only |

Prerequisites: `uv`, `bravros`, `graphifyy` ≥ the pin in `references/.graphify-version`.
