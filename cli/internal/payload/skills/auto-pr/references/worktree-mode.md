# --worktree mode — lightweight git-only isolation

Contrast: `/worktree create` provisions a full Herd site (TLS, .env, DB) for interactive
work. This recipe is disposable isolation for parallel autonomous pipelines — no dev
servers, no Herd link, no VS Code, no `npm run build` (setup-full symlinks the primary's
build output).

## Guard: never nest worktrees

Creating a worktree from inside a worktree fails with
`fatal: '/path/to/worktree/.git' is a file, not a directory`. Detect first:
`git rev-parse --git-common-dir` ≠ `git rev-parse --git-dir` → already inside one; STOP.

## Create

**Never modify the main repo's working directory or HEAD** — `git fetch` only.

```bash
BASE_BRANCH=$(git rev-parse --verify origin/homolog >/dev/null 2>&1 && echo homolog || echo main)
git fetch origin "$BASE_BRANCH"
SETUP=$(bravros worktree setup-full "$BRANCH" --from "$REPO_ROOT" --base "origin/$BASE_BRANCH")
[ "$(echo "$SETUP" | jq -r '.ready')" = "true" ] || exit 1   # STOP on not-ready
WORKTREE_PATH=$(echo "$SETUP" | jq -r '.path')               # use the derived path, never guess it
```

Idempotent — safe to re-run on a crashed pipeline. All subsequent stages run inside
`$WORKTREE_PATH`; `.planning/` is worktree-local. With `--no-install`, install deps
yourself from the repo's lockfiles.

## Cleanup after PR

```bash
cd "$REPO_ROOT"                              # leave the worktree before tearing it down
bravros worktree cleanup "$WORKTREE_PATH"    # --keep-worktree on the invocation skips this
```

An unforced cleanup **refuses** while live processes sit inside the worktree (JSON refusal,
exit 1, `live: [{pid, command}]`). Stop them and retry, or `--force`. The calling process
never counts (`liveness_note` instead); an indeterminate check warns and proceeds.

Last-resort manual removal (CLI itself broken) requires the destroy bypass —
`BRAVROS_WORKTREE_DESTROY=1 git worktree remove --force "$WORKTREE_PATH"` — the floor
guard blocks any unflagged destructive removal of a registered worktree.

