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
restore_after_link: [.agents, .claude, .ai, boost.json, AGENTS.md, CLAUDE.md]
mcp_site_path_rewrite: true
db:
  clone_name: "{repo}_wt{n}"
  dump_glob: "database/backups/*.sql.gz"
```

Placeholders: `{name}` = `<repo><id>`, `{repo}` = repo basename, `{n}` = the id.

**Do not put this block in `.bravros/config.json`** — that file round-trips through a Go struct
and silently drops unknown keys. (The legacy `.bravros.yml` is auto-migrated into it and is never
read by these scripts.)

**Every key REPLACES the script default outright — nothing merges.** A `.worktree.yml` that
defines `restore_after_link` at all silences the in-script list completely, so a path the script
learned about later (`.ai` was added long after the first lists were written) is simply not
restored in that project. The symptom is a fresh worktree that comes up dirty; **grep
`.worktree.yml` before editing any script default**, because editing the default changes nothing
while the config defines the key. Same rule for `env_isolate` and `runtime_dirs`.

**Why `.ai/` churns at all (Laravel + Boost).** `herd link` runs `php artisan boost:update`
unconditionally — it is hardcoded on `vendor/laravel/boost` plus `artisan` existing, and no env
var or flag disables it. With `"cloud": true` in `boost.json`, Boost downloads the
`deploying-laravel-cloud` skill **live from GitHub HEAD** (`laravel/cloud-cli`), so that one file
drifts from the committed copy whenever upstream changes; it is the only skill fetched remotely.
Setting `"cloud": false` stops the fetch and the committed copy survives — SkillComposer treats
`.ai/skills/*` as user skills and will not prune it. `boost:update` itself is **synchronous**: it
finishes before `herd link` returns, so a late write is never the explanation — an unlisted path is.

## Behavior notes

- **Non-Laravel repo (or no Herd):** worktree + runtime-dir clone + branch resolution + git smoke tests still run; `.env`/Herd/DB/storage steps skipped with a note; `--clone-db` refuses. Detection reads `stack.framework` from `.bravros/config.json` first (`bravros config get` when it can answer, python3 otherwise), then falls back to an `artisan` file at the repo root — a Go/Node/Python repo never triggers Herd.
- **Merge checks are content-aware** (diff vs merge-base, not literal ancestry) — squash-merged PRs and stray planning-only commits don't mislabel a shipped branch as unmerged.
- **Shared parent DB is the default** because production-sized DBs are slow to copy. `--clone-db` seeds from the newest valid local dump; `--live-dump` forces a live mysqldump.
- **Destroy** unlinks Herd FIRST (no dangling link if a later step fails), only ever drops the clone DB (never the parent's), and deletes the local branch only when its code is safe (`merged`, or `plan-only` after explicit `--force`).
- **`REDIS_PREFIX` isolates queues** — the parent's `queue:work`/Horizon never picks up worktree jobs; run one inside the worktree to process them.
- **The knowledge graph travels with the checkout** — `graphify-out/graph.json` is tracked; a session inside the worktree queries it flag-free, the parent session needs `project_path: "<worktree path>"`. `sync --merge` won't refresh it — the post-merge hook only rebuilds on the autocommit branch.
- **Post-create smoke tests warn but never tear down**; lockfile drift vs the parent HEAD is flagged before it can be committed; Herd-link Boost churn (`.agents/`, `.claude/`, `.ai/`, `boost.json`, `AGENTS.md`, `CLAUDE.md`) is reverted so the branch starts byte-clean. A tracked file whose content is identical and only its **mode** changed is restored too, whatever step flipped the bit — that net runs in every repo, Laravel or not.
- `list` `merged=` column: `main` (shipped) · base name (in staging) · `<ref>+plan⚠` (code shipped, planning-only delta dangles) · `no`; `↑N ↓N` is vs that same ref.
- **Fetch performance & safety:** Every verb (`create`, `destroy`, `list`, `sync`) guards its `git fetch` against SSH agent hangs on machines using 1Password or similar signers. Pass `--fresh` to force a refetch (default: skip within `WT_FETCH_TTL` seconds, default 300). `WT_FETCH_TIMEOUT` (default 15 seconds) is a hard watchdog — the fetch is killed on expiry rather than blocking forever. When `origin` is a GitHub remote **and** `gh auth token` exits 0, the fetch goes over HTTPS using gh's credential helper, which never consults the SSH agent; plain SSH is the timeout-guarded fallback.
- **Stale-status degradation:** A fetch that times out or fails does **not** abort the verb — it degrades merge/ahead-behind statuses to `unknown` or stale, and the verb continues. Degraded status never softens a safety gate: `destroy` still **refuses** to remove a worktree whose branch it could not prove merged; an un-fetched ref refuses exactly as being offline does today. `--force` remains the only override.

## Rollout

Edits to `skills/worktree/` in this repo do **not** reach the installed copy the agent actually executes. Changes go live only after `bravros deploy` syncs the skill into your home installation — until then a verb still runs its previous version, so measure and debug against the source scripts by path, not the installed ones.
