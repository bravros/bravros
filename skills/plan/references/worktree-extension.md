# Plan --worktree Extension

When `/plan --worktree` is invoked (or `BRAVROS_WORKTREE=true`), the dossier is created inside
an isolated worktree instead of the current checkout. Dossier shape, inline review, backlog
promotion, and event rules are identical to the standard flow.

## Path convention

```
Worktree path = <parent-dir>/<repo-name><plan-num-short>
Example: ~/Sites/paylog23  (plan #23 of the paylog repo)
```

## Guard

Already inside a worktree (`[ "$(git rev-parse --git-dir)" != "$(git rev-parse --git-common-dir)" ]`)
→ STOP and tell the user to run from the main repo. Never `git checkout` or `git pull` the
main repo — `git fetch origin` only.

## Create worktree + branch

```bash
REPO_NAME=$(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")
PARENT_DIR=$(dirname "$PWD")
NEXT_NUM=${PLAN_ID#P-}                            # P-0042 → 0042
PLAN_NUM_SHORT=$(echo "$NEXT_NUM" | sed 's/^0*//') # 0042 → 42
WORKTREE_PATH="${PARENT_DIR}/${REPO_NAME}${PLAN_NUM_SHORT}"
BRANCH_NAME="<type>/<short-description>"

bravros worktree setup "$BRANCH_NAME" --path "$WORKTREE_PATH"
```

Non-zero exit → STOP and report; do not improvise the worktree by hand.

Then `cd "$WORKTREE_PATH"`, write the dossier folder THERE (not in the main repo), append the
`created` + `reviewed` events there, and commit inside the worktree
(`bravros commit "📋 plan: add P-NNNN <slug>" .planning/`). The main repo stays untouched.
`/orchestrate` then runs from inside that worktree.

## Environment setup (only on request / `--full`)

```bash
HERD_FLAG=""
[ "$(uname)" = "Darwin" ] && command -v herd &>/dev/null && HERD_FLAG="--herd"
bravros worktree setup-full "$BRANCH_NAME" --from "$REPO_ROOT" --base "origin/homolog" $HERD_FLAG
```

**Laravel `.env` rules (hard-won):**
- **NEVER modify APP_KEY** — only `APP_URL` and `SESSION_DOMAIN`.
- macOS+Herd: each worktree gets its own `https://<repoNN>.test`; set
  `APP_URL=https://<worktree-name>.test` and blank `SESSION_DOMAIN=`.
- Linux (no Herd): `php artisan serve --port=<unique_port>` per worktree.
- Never run `herd link` in the main repo during worktree setup.

Node/JS, Python (`uv sync`), and Go deps are handled by `setup-full` from the detected
lockfile.
