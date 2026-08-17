# .worktree.yml overrides + behavior notes

## Per-project overrides

Zero config needed. Drop `.worktree.yml` next to the repo (or at the workspace root,
covering every child) only when defaults don't fit. All keys optional:

```yaml
base: homolog
env_isolate:                       # replaces the default APP_URL + REDIS_PREFIX pair
  REDIS_PREFIX: "{name}_"
  APP_URL: "https://{name}.test"
runtime_dirs: [vendor, node_modules, public/build, bootstrap/cache]
restore_after_link: [.agents, .claude, boost.json, AGENTS.md, CLAUDE.md]
mcp_site_path_rewrite: true
db:
  clone_name: "{repo}_wt{n}"
  dump_glob: "database/backups/*.sql.gz"
```

Placeholders: `{name}` = `<repo><id>`, `{repo}` = repo basename, `{n}` = the id.

**Do not put this block in `.bravros.yml`** — that file round-trips through a Go struct
and silently drops unknown keys.

## Behavior notes

- **Non-Laravel repo (or no Herd):** worktree + runtime-dir clone + branch resolution + git smoke tests still run; `.env`/Herd/DB steps skipped with a note; `--clone-db` refuses.
- **Merge checks are content-aware** (diff vs merge-base, not literal ancestry) — squash-merged PRs and stray planning-only commits don't mislabel a shipped branch as unmerged.
- **Shared parent DB is the default** because production-sized DBs are slow to copy. `--clone-db` seeds from the newest valid local dump; `--live-dump` forces a live mysqldump.
- **Destroy** unlinks Herd FIRST (no dangling link if a later step fails), only ever drops the clone DB (never the parent's), and deletes the local branch only when its code is safe (`merged`, or `plan-only` after explicit `--force`).
- **`REDIS_PREFIX` isolates queues** — the parent's `queue:work`/Horizon never picks up worktree jobs; run one inside the worktree to process them.
- **The knowledge graph travels with the checkout** — `graphify-out/graph.json` is tracked; a session inside the worktree queries it flag-free, the parent session needs `project_path: "<worktree path>"`. `sync --merge` won't refresh it — the post-merge hook only rebuilds on the autocommit branch.
- **Post-create smoke tests warn but never tear down**; lockfile drift vs the parent HEAD is flagged before it can be committed; Herd-link Boost churn (`.agents/`, `boost.json`, `AGENTS.md`, `CLAUDE.md`) is reverted so the branch starts byte-clean.
- `list` `merged=` column: `main` (shipped) · base name (in staging) · `<ref>+plan⚠` (code shipped, planning-only delta dangles) · `no`; `↑N ↓N` is vs that same ref.
