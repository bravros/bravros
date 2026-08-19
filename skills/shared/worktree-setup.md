# Worktree Setup and Isolation

Worktree creation and teardown for `/auto-pr --worktree`. Contrast: `/plan --worktree`
creates a full Herd site (SSL, browser testing, VS Code) for interactive work — this
recipe is lightweight git-only isolation for parallel autonomous pipelines, auto-removed
after the PR.

## Step 0: Sanity Echo (mandatory)

Run and show output before anything else — all later paths derive from `$PWD`:

```bash
pwd && echo "$(git branch --show-current)"
```

If either differs from what the dispatch prompt expects, STOP and report; do not create
the worktree.

## Step 1: Guard Against Nested Worktrees

Creating a worktree from inside a worktree fails with
`fatal: '/path/to/worktree/.git' is a file, not a directory`:

```bash
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  COMMON=$(git rev-parse --git-common-dir 2>/dev/null)
  GITDIR=$(git rev-parse --git-dir 2>/dev/null)
  if [ "$COMMON" != "$GITDIR" ]; then
    echo "❌ ERROR: Already inside a worktree. Run from the main repo."
    # STOP. Do NOT proceed to worktree creation.
  fi
fi
```

## Step 2: Path Convention

Worktrees live in a `.worktrees/` sibling of the repo, named `<repo>-<branch-slug>`:

```
Main repo:  ~/Sites/elopool
Branch:     feat/user-authentication
Worktree:   ~/Sites/.worktrees/elopool-feat-user-authentication
```

## Step 3: Fetch and Create (one verb)

**Do NOT modify the main repo's working directory or HEAD** — `git fetch` only, never
`git checkout`/`git pull` in the main repo.

`bravros worktree setup-full` handles `git worktree add` + framework-aware dep install +
`.env` copy + asset symlink, all idempotent (safe to re-run on a crashed pipeline):

```bash
BASE_BRANCH=$(git rev-parse --verify origin/homolog 2>/dev/null && echo "homolog" || echo "main")
git fetch origin "$BASE_BRANCH"

SETUP=$(bravros worktree setup-full "$BRANCH" \
  --from "$REPO_ROOT" \
  --base "origin/$BASE_BRANCH")

READY=$(echo "$SETUP" | jq -r '.ready')
if [ "$READY" != "true" ]; then
  echo "❌ setup-full did not reach ready state — inspect JSON above."
  # STOP. Do NOT proceed.
fi

# Use the path the verb actually derived:
WORKTREE_PATH=$(echo "$SETUP" | jq -r '.path')
```

Autonomous-pipeline policy inside the worktree: **no `npm run build`** (setup-full
symlinks primary's build output), **no dev servers, no Herd linking, no VS Code**. With
`--no-install`, install deps yourself (composer/npm per the repo's lockfiles).

All subsequent pipeline stages run inside `$WORKTREE_PATH`; `.planning/` is
worktree-local.

## Cleanup After PR

Once the PR is created and the review loop finishes, all code is on the remote branch —
the worktree is disposable:

```bash
cd "$REPO_ROOT"   # leave the worktree before tearing it down
bravros worktree cleanup "$WORKTREE_PATH"
```

Pass `--keep-worktree` on the skill invocation to skip cleanup (debugging / inspection).

**Liveness guard (refusal shape):** an unforced `bravros worktree cleanup` refuses
teardown when live processes (cwd or open files) are still inside the worktree — it
prints a JSON refusal and exits 1:

```json
{
  "error": "2 live process(es) inside worktree — refusing teardown",
  "hint": "stop them or re-run with --force",
  "live": [ { "pid": 12345, "command": "node" } ]
}
```

- On refusal: stop the listed processes (or re-run with `--force`) and retry.
- The calling process never counts toward refusal — it is reported via
  `liveness_note: "you are inside this worktree"` instead.
- When the check can't run (no `lsof`/`/proc`, or timeout), `liveness` is `"unknown"`
  and cleanup proceeds on a warn basis — an indeterminate check never blocks teardown.
- `--force` bypasses the guard (and the merge check); `live_processes` is still reported.

## Stuck / Orphaned Worktrees

```bash
# Preferred: the cleanup verb sets the destroy bypass internally.
bravros worktree cleanup "$WORKTREE_PATH" --force

# Very-last-resort hard manual delete (only if the CLI itself is broken).
# BRAVROS_WORKTREE_DESTROY=1 is required — the floor guard blocks any
# unflagged destructive removal of a git-registered worktree.
BRAVROS_WORKTREE_DESTROY=1 git worktree remove --force "$WORKTREE_PATH"
```
