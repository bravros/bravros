# Worktree Setup and Isolation

This document defines the worktree creation and management for `/auto-pr-wt` and `/auto-merge` skills.

**Contrast with:**
- `/auto-pr` which runs in the main repo (no isolation)
- `/plan-wt` which creates a full Herd site (includes SSL, browser testing)

## Why Worktrees?

Running multiple `/auto-pr` instances on the same repo causes git conflicts:
- All instances share the same working directory
- `git add` can pick up unintended changes from other agents
- `.planning/` directory conflicts on concurrent writes
- Feature branches can accidentally include code from parallel pipelines

Worktrees solve this: each pipeline gets its own isolated directory with its own git state.

## Worktree vs. Plan-WT

| Aspect | `/auto-pr-wt` Worktree | `/plan-wt` Worktree |
|--------|----------------------|-------------------|
| **Isolation** | Git worktree only | Git worktree + Herd site + VS Code |
| **Herd Link** | ❌ None | ✅ Yes (example.test URL) |
| **VS Code** | ❌ Not opened | ✅ Opened for interactive development |
| **SSL/HTTPS** | ❌ No | ✅ Yes (for browser testing) |
| **Weight** | Lightweight | Full dev environment |
| **Use Case** | Parallel autonomous pipelines | Interactive development with testing |
| **Cleanup** | Auto-removes worktree after PR | User manually closes VS Code + worktree |

## Step 1: Guard Against Nested Worktrees

NEVER allow the skill to run inside an existing worktree:

```bash
echo "🤖 [skill:2] creating lightweight worktree"

# Guard: refuse to run if already inside a worktree
TOPLEVEL=$(git rev-parse --show-toplevel 2>/dev/null)
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  COMMON=$(git rev-parse --git-common-dir 2>/dev/null)
  GITDIR=$(git rev-parse --git-dir 2>/dev/null)
  if [ "$COMMON" != "$GITDIR" ]; then
    echo "❌ ERROR: Already inside a worktree. Run from the main repo."
    # STOP. Do NOT proceed to worktree creation.
  fi
fi
```

This prevents the error: "fatal: '/path/to/worktree/.git' is a file, not a directory"

## Step 2: Prepare Worktree Path

```bash
REPO_NAME=$(basename "$PWD")
REPO_ROOT="$PWD"
PARENT_DIR=$(dirname "$PWD")

# Generate branch name from description
BRANCH="feat/$(echo "$DESCRIPTION" | tr ' ' '-' | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]//g' | head -c 50)"

# Worktree path: parent-dir / .worktrees / repo-branch-slug
WORKTREE_SLUG=$(echo "$BRANCH" | tr '/' '-')
WORKTREE_PATH="${PARENT_DIR}/.worktrees/${REPO_NAME}-${WORKTREE_SLUG}"

# Ensure .worktrees directory exists
mkdir -p "${PARENT_DIR}/.worktrees"
```

**Example:**
```
Main repo:  ~/Sites/elopool
Branch:     feat/user-authentication
Worktree:   ~/Sites/.worktrees/elopool-feat-user-authentication
```

## Step 3: Fetch and Create Worktree

**CRITICAL: Do NOT modify the main repo's working directory or HEAD.**

```bash
# Fetch latest from remote WITHOUT modifying main repo's HEAD
BASE_BRANCH=$(git rev-parse --verify origin/homolog 2>/dev/null && echo "homolog" || echo "main")
git fetch origin "$BASE_BRANCH"

# Create worktree from remote ref — main repo state stays untouched
bravros worktree setup "$BRANCH" --path "$WORKTREE_PATH" --base "origin/$BASE_BRANCH"
```

**Rules:**
- Use `git fetch` only — do NOT `git checkout` or `git pull` in the main repo
- Clone the worktree from `origin/$BASE_BRANCH`, not a local branch
- This ensures the main repo's HEAD never changes

## Step 4: Verify Worktree Created

The `bravros worktree setup` command handles verification internally. If it exits with a non-zero status, the worktree was not created — stop and report the failure. Do NOT proceed.

## Step 5: Install Dependencies

All subsequent steps run inside `$WORKTREE_PATH`. Install what's needed:

```bash
cd "$WORKTREE_PATH"

FRAMEWORK=$(bravros meta --field stack.framework)
HAS_ASSETS=$(bravros meta --field stack.has_assets)

# PHP projects
if [ "$FRAMEWORK" = "laravel" ]; then
  herd composer install --no-interaction 2>/dev/null || true
fi

# Node projects
if [ "$FRAMEWORK" = "nextjs" ] || [ "$FRAMEWORK" = "node" ] || [ "$FRAMEWORK" = "expo" ]; then
  npm install 2>/dev/null || true
fi

# Build assets (non-interactive, no dev server)
if [ "$HAS_ASSETS" = "true" ]; then
  npm run build 2>/dev/null || true
fi
```

**NO `npm run dev`** — don't start dev servers in autonomous mode. Just build assets.

**NO Herd linking** — no `herd link` or `herd secure` needed. This is lightweight isolation.

**NO VS Code** — don't open editors in autonomous mode.

## Step 6: Run Pipeline Inside Worktree

All subsequent pipeline stages (plan → review → execute → check → PR) run inside `$WORKTREE_PATH`:

```bash
cd "$WORKTREE_PATH"

# All commits happen inside the worktree
# All feature branch work is isolated
# .planning/ directory is worktree-local
```

**Git behavior inside worktree:**
- Commits go to the feature branch
- `git push origin $BRANCH` pushes to the remote feature branch
- `git diff` shows changes relative to the worktree's base
- PR creation references the feature branch (safely isolated)

## Step 7: Auto-Cleanup After PR

Once the PR is created and review loop finishes (or after auto-merge):

```bash
echo "🤖 [skill:9] cleaning up worktree"

# Return to main repo
cd "$REPO_ROOT"

# Remove worktree (all code is safely on remote branch now)
if [ -z "$KEEP_WORKTREE" ]; then
  bravros worktree cleanup "$WORKTREE_PATH"
  echo "✅ Worktree cleaned up: $WORKTREE_PATH"
else
  echo "⚠️ Worktree kept (--keep-worktree): $WORKTREE_PATH"
fi
```

**Why cleanup is safe:**
- All code is committed and pushed to the feature branch
- The PR points to the remote branch
- The `.planning/` directory is archived locally if needed
- The worktree's git history is part of the main repo's reflogs

**Why --keep-worktree flag exists:**
- User wants to debug the worktree manually
- User wants to inspect the final state before cleanup
- User is running auto-merge and wants to keep worktrees for inspection

## Step 8: Guard Against Double Cleanup

If the pipeline is interrupted and restarted:

```bash
# Check if worktree still exists
if [ ! -d "$WORKTREE_PATH" ]; then
  echo "Worktree already cleaned up"
  # Skip cleanup, proceed to next step
else
  # Cleanup as normal
  bravros worktree cleanup "$WORKTREE_PATH"
fi
```

## Error Handling

### Worktree creation fails

If `bravros worktree setup` exits non-zero:
- Check for common issues:
  1. Already exists: run `bravros worktree cleanup "$WORKTREE_PATH"` then retry
  2. Invalid path: check PARENT_DIR exists and is writable
  3. Git corruption: run `git fsck` in main repo
- STOP. Do NOT proceed.

### Dependency install fails in worktree

```bash
cd "$WORKTREE_PATH"
FRAMEWORK=$(bravros meta --field stack.framework)
if [ "$FRAMEWORK" = "laravel" ] && ! herd composer install --no-interaction 2>/dev/null; then
  echo "⚠️ Composer install failed in worktree — attempting cleanup"
  cd "$REPO_ROOT"
  bravros worktree cleanup "$WORKTREE_PATH"
  # STOP. Report failure.
fi
```

### Force-cleanup if stuck

If a worktree gets stuck or orphaned:

```bash
# Manual cleanup (user or script)
bravros worktree cleanup "$WORKTREE_PATH"

# Or delete directory manually if CLI fails
rm -rf "$PARENT_DIR"/.worktrees/...
```

## Flags

### --keep-worktree

By default, worktree is cleaned up after PR creation. Pass `--keep-worktree` to keep it:

```bash
/auto-pr-wt --keep-worktree <description>
```

Use case: Debugging, final verification, auto-merge inspection.

### --no-install

Skip dependency installation in worktree:

```bash
/auto-pr-wt --no-install <description>
```

Use case: Lightweight runs where dependencies are already installed, or when you want to speed up worktree creation.

### --auto-merge (auto-merge only)

Passed by `/auto-merge` to merge PR immediately after creation:

```bash
/auto-pr-wt --auto-merge <description>
```

See `batch-loop.md` for details.

## Batch-Flow Worktree Management

In `/auto-merge`, multiple worktrees may be created for parallel/sequential plan execution:

```
.worktrees/
├── elopool-feat-user-auth
│   └── .git
├── elopool-feat-api-versioning
│   └── .git
└── elopool-feat-performance-opt
    └── .git
```

Each worktree is independent. After each plan completes:
- PR is merged
- Worktree is cleaned up (unless --keep-worktree)
- Main repo state remains clean
- Next plan's worktree is created fresh

**Crash recovery:** If auto-merge is interrupted, crashed worktrees can be cleaned manually:

```bash
# Clean a specific stuck worktree
bravros worktree cleanup "$WORKTREE_PATH"

# Or delete directory manually if CLI fails
rm -rf "$PARENT_DIR"/.worktrees/elopool-feat-something
```

## Worktree Debugging

To inspect a worktree manually (when using --keep-worktree):

```bash
cd ~/.../elopool-feat-something
git log --oneline
git diff origin/main...HEAD
```

To manually cleanup afterward:

```bash
cd ~/Sites/elopool  # Return to main repo
bravros worktree cleanup ~/.../elopool-feat-something
```

## References

- **pipeline.md** — Core shared pipeline stages
- **mode-autonomous.md** — Autonomous behavior (used by /auto-pr-wt)
- **batch-loop.md** — Batch orchestration (uses worktree isolation)
