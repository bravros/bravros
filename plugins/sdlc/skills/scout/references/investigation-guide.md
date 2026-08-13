# Investigation Guide

Reference for `/root-cause` — MCP posture, graphify surface detection, the Step-6 routing matrix, and the handoff contract.

## MCP posture

- **Laravel Boost MCP** — optional. Artisan one-shots are the default probe surface (see `certification.md`); if `mcp_*boost*__*` tools are already connected you MAY use them, but never require or install Boost — its tool schemas and outputs cost tokens artisan doesn't.
- **Sentry MCP** — optional. When present: `search_issues` with the error message/exception class, `search_events` for count/frequency; include the issue URL in findings.
- Never fail because an MCP is absent — fall back to artisan probes and file-based investigation.

## Graphify surface detection

Graph present = `graphify-out/graph.json` or a `.graphify` file. Prefer the user-scoped MCP (`mcp_graphify__query_graph {question}`, then `get_neighbors`/`shortest_path` for blast radius; pass `project_path` from outside the repo). CLI backup: `graphify query "<symptom>" --graph graphify-out/graph.json`. A `.graphifyignore` with no graph means config only — use grep/git instead. Graph answers are **leads**: the graph can be stale, incomplete, or mislabelled; the source always wins.

## Routing matrix (which option to recommend first)

| Condition | Recommend |
|-----------|-----------|
| Certified, small, single-file fix | `/quick — fix now` |
| Certified, simple, can wait | Add to backlog |
| Certified, 3+ files OR architectural OR severity high+ | Escalate to `/plan` |
| Certified, external dependency, blocking, production-critical | Backlog + GH issue (`gh issue create --title "🐛 fix: …" --body "$(cat $DEBUG_DIR/diagnosis.md)" --label bug`; record the issue # in the B-file's `github:` frontmatter) |
| **`UNCERTIFIED`** (round cap hit) | Backlog for deeper investigation — surface `/quick` last or not at all |
| Investigation is sufficient as the record | Leave as-is |

## Debug Handoff payload

```
## Debug Handoff
**Investigation:** $DEBUG_ID ($DEBUG_DIR)
**Diagnosis file:** $DEBUG_DIR/diagnosis.md
**Root cause:** {one-line summary}
**Certification:** {one-line proof summary | UNCERTIFIED — N rounds, see report}
**Affected files:** {list}
**Fix direction:** {what needs to change — intent, not code}
**Severity:** {critical/high/medium/low}
**Branch strategy:** quick | plan | backlog-only
**debug_commit:** $DEBUG_COMMIT
```

## Receiver contract

The receiving path (backlog file, `/plan`, `/quick`) MUST:

1. Record `debug: $DEBUG_ID` in its own frontmatter.
2. Include `debug_commit: $DEBUG_COMMIT` in its commit message.
3. Once it has its own artifact ID, rewrite the report's `linked_to` from `pending-handoff` to the real ID — that completes the bidirectional link.

Plan route: root cause → Goal, fix direction → Phases, blast radius → scope; pass the full diagnosis. Backlog route: write the `B-NNNN` file per `.planning/CONVENTIONS.md` (id via `bravros nextid`, one `created` event appended to `.planning/events.jsonl`) with root cause + file paths + proof transcript in the body.
