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
- **Fetch performance & safety:** Every verb (`create`, `destroy`, `list`, `sync`) guards its `git fetch` against SSH agent hangs on machines using 1Password or similar signers. Pass `--fresh` to force a refetch (default: skip within `WT_FETCH_TTL` seconds, default 300). `WT_FETCH_TIMEOUT` (default 15 seconds) is a hard watchdog — the fetch is killed on expiry rather than blocking forever. When `origin` is a GitHub remote **and** `gh auth token` exits 0, the fetch goes over HTTPS using gh's credential helper, which never consults the SSH agent; plain SSH is the timeout-guarded fallback.
- **Stale-status degradation:** A fetch that times out or fails does **not** abort the verb — it degrades merge/ahead-behind statuses to `unknown` or stale, and the verb continues. Degraded status never softens a safety gate: `destroy` still **refuses** to remove a worktree whose branch it could not prove merged; an un-fetched ref refuses exactly as being offline does today. `--force` remains the only override.

## Rollout

Edits to `skills/worktree/` in this repo do **not** reach the installed copy the agent actually executes. Changes go live only after `bravros deploy` syncs the skill into your home installation — until then a verb still runs its previous version, so measure and debug against the source scripts by path, not the installed ones.
