# Briefing: context

## Flow

1. **Detect the stack** from manifest + lock files (field checklist: `references/stack-detection.md`; lock files for exact versions) → `STACK_JSON`. Refresh the `<!-- BRAVROS:CONTEXT:STACK START/END -->` managed block in the root CLAUDE.md with it (idempotent — skip when byte-equal ignoring the timestamp; other skills read this block instead of re-detecting).
2. **Context7 enrichment (optional)**: `mcp_context7__resolve-library-id` with name + exact version (e.g. `"laravel 13.2.0"`), then `query-docs` for directory conventions and testing patterns — informs which subdirectory files to generate. Unavailable or `--no-context7` → continue with built-in conventions. Laravel: if laravel/boost is installed, run `php artisan boost:update`; if not, ask before installing.
3. **Scan**: find existing CLAUDE.md files (excluding vendor/node_modules/.git); map directories that warrant one (5+ files with shared patterns, non-obvious rules, gotchas, external API integrations). Graph-enabled project → let `mcp_graphify__god_nodes` + `get_community` pick the clusters (one real subsystem ≠ one folder), then confirm against the code — a stale community label will invent a subsystem that no longer exists.
4. **Dispatch** the workers:
   ```bash
   mkdir -p .claude/workflows && cp -f ~/.bravros/skills/context/scripts/context-authors.js .claude/workflows/context-authors.js
   ```
   ```
   Workflow({ name: 'context-authors', args: { stack_json: STACK_JSON,
     clusters: [ { name, dirs: [...], template: '<per-cluster conventions>', docs: '<Context7 + test-runner notes>' } ] } })
   ```
   Pass operator test conventions from `references/test-runners.md` via `docs`. Returns `{ clusters, results }` — each entry `{ cluster, files_written?, files_audited?, staleness? }` (dead workers → `null`, filtered).
5. **Audit findings → approval**: staleness (wrong versions, deprecated APIs, dead paths, wrong test patterns) and README drift are presented via `ask_question` — nothing overwritten without explicit confirmation. Announce before waiting:
   <!-- announce-template: "Auditoria de contexto concluída, aguardando aprovação das atualizações sugeridas. Projeto {PROJECT}." -->
   ```bash
   bravros ha say --force "Auditoria de contexto concluída, aguardando aprovação das atualizações sugeridas. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
   ```
6. **Report**: stack detected, Context7 usage, files created/updated/unchanged, directories skipped, README findings.
