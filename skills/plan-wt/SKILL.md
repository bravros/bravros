---
name: plan-wt
description: >
  Create a plan with isolated git worktree for parallel development — separate directory,
  separate dev URL, separate VS Code window. Use this skill whenever the user says
  "/plan-wt", "plan with worktree", "worktree plan", "parallel plan", or any request
  to create a plan with an isolated working environment. Also triggers on "plan in worktree",
  "isolated plan", "plan in a separate worktree", "keep my branch clean",
  "plan without affecting current work", or when the user needs to work on a feature while
  keeping the main repo available for other work. After completion, use /complete to clean
  up the worktree.
---

# Plan-WT: Create Plan with Isolated Worktree

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

Create a plan and branch in an isolated git worktree — a separate directory with its own dev URL (Herd on macOS, `php artisan serve` on Linux), so you can work on a feature without touching the main repo.

| Variant | Skill | Setup | Use Case |
|---------|-------|-------|----------|
| `/plan` | plan | Branch only | Default — most features, fixes, single-threaded work |
| `/plan-wt` | plan-wt | Worktree + Branch | Parallel development, isolated environment, separate dev URL (Herd on macOS, artisan serve on Linux) |

**When to use /plan-wt instead of /plan:**
- You need to keep working in the main repo while a feature is in progress
- Long-running feature that you'll context-switch in and out of
- You want a separate `repoNN.test` URL for testing (macOS/Herd) or a separate port (Linux)
- Multiple features in parallel (each in its own worktree)

## Critical Rules

1. **Read the template first.** Read `references/plan-template.md` (bundled with this skill) at Step 1. Never create a plan without it.
2. **Use sequential thinking.** Call `mcp__sequential-thinking__sequentialthinking` at Step 2.
3. **Ask when ambiguous.** Use `AskUserQuestion` at Step 2 if anything is unclear.
4. **Follow step order.** Steps build on each other — skipping breaks the data flow.
5. **NEVER modify the main repo.** No checkout, no pull, no commits in the main working directory. All changes happen inside the worktree.
6. **Create worktree FIRST, then plan inside it.** The plan file is created and committed inside the worktree directory, not the main repo.

## Backlog Integration

If `$ARGUMENTS` is a number (e.g., `/plan-wt 3`), it refers to a backlog idea:

1. Check `.planning/backlog/` and `.planning/backlog/archive/` for that idea number
2. Use its content as the basis for the plan
3. After creating the plan:
   - Update the backlog idea status to `> **Status:** Planned`
   - Add plan link: `> **Plan:** NNNN-<type>-<description>-todo.md`
   - Move the file to `.planning/backlog/archive/`


## Step 1/6: Parallel Data Gather

Run these **simultaneously in a single step**:

1. `~/.claude/bin/bravros meta --reserve` — returns base_branch, project, git_remote, next_num, backlog_next as JSON (atomic ID reservation included)
2. Read `references/plan-template.md` (bundled with this skill)
3. `git fetch origin` — fetch latest without modifying working directory

**Guard:** If already inside a worktree (`git rev-parse --show-toplevel` contains `.worktrees`), STOP and tell the user to run from the main repo.

Do NOT run `git checkout` or `git pull` in the main repo. Do NOT create a plan from memory. Do NOT proceed without reading the template.

## Step 2/6: Explore + Sequential Thinking

**First — launch Explore agents in parallel:**
- One agent per affected domain (models, services, controllers, tests, etc.)
- Multiple agents run simultaneously. Always set `model: "sonnet"` on these Explore agent calls.
- Skip for very simple tasks

**Then — sequential thinking** with all gathered data:

You MUST use `mcp__sequential-thinking__sequentialthinking`:

- Analyze $ARGUMENTS and Explore findings
- Break into phases with clear acceptance criteria and phase dependencies
- Fill `## Context Pack` (constraints, key paths, verification commands)
- Apply plan size guardrail (>6 phases, >25 tasks → split into Plan A + Plan B)
- Use `AskUserQuestion` for genuine ambiguities ONLY

## Step 3/6: Create Worktree + Branch

```bash
REPO_NAME=$(basename "$PWD")
REPO_ROOT="$PWD"
PARENT_DIR=$(dirname "$PWD")
PLAN_NUM_SHORT=$(echo $NEXT_NUM | sed 's/^0*//')
WORKTREE_PATH="${PARENT_DIR}/${REPO_NAME}${PLAN_NUM_SHORT}"
WORKTREE_NAME="${REPO_NAME}${PLAN_NUM_SHORT}"
BRANCH_NAME="<type>/<short-description>"

# Create branch and worktree via CLI (handles creation, verification, and rebase)
~/.claude/bin/bravros worktree setup "$BRANCH_NAME" --path "$WORKTREE_PATH"
```

**Worktree Gate:** If the command exits non-zero → **STOP**. Do NOT proceed. Report the failure and ask user to resolve.

## Step 4/6: Create Plan Inside Worktree

```bash
cd "$WORKTREE_PATH"
```

**Session tracking:** Use `${CLAUDE_SESSION_ID}` (auto-substituted by Claude Code) for the session ID. The `session:` single field is set by `/plan-approved` when execution begins — use `null` at plan creation time.

Create the plan file inside the worktree:
```bash
cat > .planning/${NEXT_NUM}-<type>-<slug>-todo.md <<'EOF'
---
number: ${NEXT_NUM}
type: <type>
title: <title>
session: null
---
# Plan Content
> **Worktree:** $WORKTREE_PATH
EOF

git add .planning/ && git commit -m "📋 plan: add ${NEXT_NUM}-<type>-<short-description>"
```

The plan is committed inside the worktree on the feature branch — the main repo is untouched.

## Step 5/6: Present Plan + Deferred Environment Setup

```bash
command -v code &>/dev/null && code "$WORKTREE_PATH" && code "$WORKTREE_PATH/$PLAN_FILE"
```

Output:
```
Plan created and worktree ready!
Plan:      .planning/NNNN-type-description-todo.md
Worktree:  /path/to/repoNN
Branch:    type/description
Phases:    N phases, X tasks
Main repo: untouched ✓
```

**STOP. Use `AskUserQuestion`:**

- **Question:** "Plan and worktree ready. What next?"
- **Option 1:** "Set up environment (install deps + Herd URL) then review" — runs Step 6 then user reviews
- **Option 2:** "I'll review in VS Code first" — User reviews plan, defers environment setup
- **Option 3:** "Skip environment — run /plan-review" — worktree-only mode, no installs

## Step 6/6: Environment Setup (on-demand)

Only run this step when user requests it (Option 1 above, or `--full` flag).

```bash
cd "$WORKTREE_PATH"

# Copy .env from main repo
cp "$REPO_ROOT/.env" .env

# Update APP_URL and SESSION_DOMAIN only — NEVER touch APP_KEY
WORKTREE_NAME="$WORKTREE_NAME" python3 - <<'PY'
import os
from pathlib import Path
env_path = Path('.env')
txt = env_path.read_text(encoding='utf-8')
worktree_name = os.environ.get('WORKTREE_NAME', '').strip()
if not worktree_name:
    raise SystemExit('WORKTREE_NAME not set')
lines = txt.splitlines(True)
out = []
seen_app_url = False
seen_session_domain = False
for line in lines:
    if line.startswith('APP_URL='):
        out.append(f'APP_URL=https://{worktree_name}.test\n')
        seen_app_url = True
    elif line.startswith('SESSION_DOMAIN='):
        out.append('SESSION_DOMAIN=\n')
        seen_session_domain = True
    else:
        out.append(line)
if not seen_app_url:
    out.append(f'APP_URL=https://{worktree_name}.test\n')
if not seen_session_domain:
    out.append('SESSION_DOMAIN=\n')
env_path.write_text(''.join(out), encoding='utf-8')
PY

# Run installs with 120s timeout to prevent hanging
if command -v herd &>/dev/null; then
  timeout 120 herd composer install --no-interaction &
else
  timeout 120 composer install --no-interaction &
fi
timeout 120 npm install &
wait
npm run build

# Dev URL setup — macOS (Herd) or Linux (artisan serve)
if command -v herd &>/dev/null; then
  # macOS: use Herd for a dedicated .test domain
  herd link && herd secure && herd open
else
  # Linux: no Herd available — use php artisan serve on a dedicated port
  echo "ℹ️  Herd not available. Start the dev server with:"
  echo "   php artisan serve --port=8001"
  echo "   (choose a unique port per worktree to avoid conflicts)"
fi
```

**Environment rules:**
- NEVER modify APP_KEY (preserves Spatie encrypted data)
- Only modify APP_URL and SESSION_DOMAIN
- On macOS with Herd: each worktree gets a `repoNN.test` domain
- On Linux: run `php artisan serve --port=<unique_port>` per worktree

## Completion Flow

```
/plan-wt → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish → /complete (from main repo)
```

`/complete` cleans up the worktree: herd unlink (if applicable), worktree remove, branch delete.

## Flags

- `--full` / `-f`: Run environment setup (installs + dev URL) immediately without asking
- `--no-install` / `-ni`: Create worktree but skip all installs and dev URL setup entirely (works on both platforms)
- `--no-herd` / `-nh`: Run installs but skip Herd link/secure/open (macOS only — on Linux this flag is a no-op)

## Rules

- NEVER modify the main working directory — no checkout, no pull, no commits
- Create worktree FIRST, then create plan INSIDE the worktree
- Worktree = repo name + plan number (e.g., paylog23), in parent directory
- Only modify APP_URL and SESSION_DOMAIN in .env — NEVER touch APP_KEY
- Break into logical phases with acceptance criteria
- After modifying plan files inside worktree, run `git add .planning/ && git commit -m "..."` to commit plan changes
- Environment setup is deferred by default — runs only when user opts in or uses --full flag

Use $ARGUMENTS as the feature/fix description to plan.