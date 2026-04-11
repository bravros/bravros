# Batch Flow: Multi-Plan Sequential Execution

This document defines the multi-plan orchestration for `/auto-merge` (auto-merge) skill.

**Contrast with:**
- `/auto-pr` (auto-pr) which runs a single plan
- `/auto-pr-wt` (auto-pr-wt) which runs a single plan in a worktree
- `pipeline.md` for shared core stages

## Why Batch Flow?

Building an MVP requires 5-10 sequential plans. Running `/auto-pr` manually for each means:
- Re-entering the command 5-10 times
- Manual branch switching and context management
- No crash recovery
- Cascading merge conflicts by plan 3-4

`/auto-merge` automates the entire loop with context management, merge chain automation, and crash recovery.

## Homolog as Rolling Mirror of Main

During batch execution, `homolog` is treated as a **rolling mirror of main** — after each plan's PR is merged into `main`, the batch resets `homolog` to `origin/main`.

**Why this is safe:**
- In a sequential batch, there's no parallel development on `homolog`
- All work flows through feature branches
- Letting `homolog` accumulate commits independently causes divergence and conflicts by plan 3-4
- Resetting to `main` after each merge guarantees the next plan branches from the latest merged state

**IMPORTANT: This is only safe in sequential batch context.** Outside of `/auto-merge`, `homolog` should NEVER be force-reset without explicit user approval.

## Step 1: Parse Arguments and Discover Plans

```bash
echo "🤖 [auto-merge:1] initializing batch pipeline"
```

Parse $ARGUMENTS for:
- `N-M` or `N M` → plan range (e.g., `2-6` = plans 0002 through 0006)
- `--no-merge` → disable auto-merge between plans (default: merge is ON)
- `--effort-budget Nm` → max effort per plan (e.g., `30m`)
- `--skip-completed` → skip `Completed` plans (default: true)

**Discover plan files:**
```bash
ls .planning/[0-9]*-*-todo.md | sort
```

Filter to requested range. Build execution queue.

**Default behavior:** Auto-merge is ON. Each plan's feat→homolog PR merges before the next plan starts, preventing cascading conflicts.

## Step 1B: Backlog Promotion (Fallback Only)

This step runs **only when** plan discovery finds zero `*-todo.md` files matching the requested range. It is a fallback, not the default path.

**Partial-range boundary:** If `/auto-merge 3-6` finds plans 3 and 4 but IDs 5-6 only exist as backlog items, Step 1B does NOT trigger. Plans 5-6 stay in backlog. Auto-promotion only activates when the entire range has no plans.

For each ID in the requested range that has no plan file:

1. Check `.planning/backlog/` for a matching backlog item:
   ```bash
   ls .planning/backlog/NNNN-*.md 2>/dev/null
   ```

2. If a backlog item exists, auto-promote it:
   - Read the backlog content
   - Dispatch `/plan --auto` with the backlog as input
   - Archive the backlog item:
     ```bash
     mv .planning/backlog/NNNN-*.md .planning/backlog/archive/
     ```

3. After promoting all found items, re-run plan discovery to build execution queue

4. If no backlog items found either, stop and report which IDs have neither plan nor backlog

**Note:** This allows `/auto-merge 5-8` to accept raw backlog IDs — they auto-promote to full plans before execution.

## Step 2: Pre-flight Checks

```bash
echo "🤖 [auto-merge:2] pre-flight checks"
```

Verify:
1. All plan files in range exist (after backlog promotion)
2. Git status is clean (no uncommitted changes)
3. On correct base branch (`homolog` or `main`)
4. Plans in correct sequence (no gaps)

**Log the batch plan using emoji table format:**

```
📋 Batch Pipeline — Plans NNNN to MMMM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Plan NNNN: description               — ✅ COMPLETED
Plan NNNN+1: description             — ⏳ QUEUED
Plan NNNN+2: description             — ⏳ QUEUED
...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: X plans | Y completed | Z to execute
Auto-merge: ON (use --no-merge to disable)
```

## Step 3: Execute Loop

For each plan in the queue:

### 3A: Check if Already Completed

```bash
STATUS=$(grep '^status:' "$PLAN_FILE" | head -1 | awk '{print $2}')
if echo "$STATUS" | grep -qi "completed"; then
  echo "🤖 [auto-merge:3] skipping plan $PLAN_NUM — already completed"
  continue
fi
```

### 3B: Prepare Base Branch

```bash
echo "🤖 [auto-merge:3] starting plan $PLAN_NUM ($CURRENT/$TOTAL)"

# Ensure on homolog branch and up-to-date
git checkout homolog && git pull origin homolog

# Push base branch to remote (worktrees clone from remote)
# If remote is stale, worktree diverges
git push origin homolog

# Verify feature branch exists or will be created by /plan
```

### 3C: Execute auto-pr-wt for This Plan

**ALWAYS use `/auto-pr-wt` (worktree isolation), NEVER `/auto-pr`.**

Each plan runs in its own worktree to prevent `.planning/` merge conflicts. Without isolation, every `/finish` creates conflicts because all branches share `.planning/` directory.

Dispatch auto-pr-wt as a subagent. **Always pass `--auto-merge`** unless `--no-merge` was set:

```
Agent({
  description: "auto-pr-wt plan NNNN",
  prompt: "Run /auto-pr-wt --auto-merge for plan file: $PLAN_FILE.
           This is plan $CURRENT of $TOTAL in a batch run.
           The --auto-merge flag means: merge the PR immediately after creation and skip the review loop.
           [paste full auto-pr-wt skill instructions]",
  mode: "auto"
})
```

Wait for completion. Capture:
- PR URL and number
- Merge status
- Any issues or warnings

### 3D: Merge feat→homolog (DEFAULT)

This step runs by default after every plan (unless `--no-merge` set). Ensures the next plan starts from clean homolog with all previous changes merged, preventing cascading merge conflicts.

After auto-pr-wt completes:

```bash
# Check if auto-pr-wt already merged via --auto-merge
PR_NUM=$(gh pr list --base homolog --head "$BRANCH" --state merged --json number -q '.[0].number')
if [ -n "$PR_NUM" ]; then
  echo "PR #$PR_NUM already merged by auto-pr-wt --auto-merge — skipping"
else
  # Merge feat → homolog
  PR_NUM=$(gh pr list --base homolog --head "$BRANCH" --state open --json number -q '.[0].number')
  if [ -n "$PR_NUM" ]; then
    bravros merge-pr "$PR_NUM" --auto-resolve-planning
  fi
fi

# Reset homolog to mirror main — safe because every plan has just merged into main
# This prevents homolog from drifting and ensures next plan branches from clean base
git checkout homolog \
  && git reset --hard origin/main \
  && git push origin homolog --force-with-lease \
  && git fetch origin homolog
```

**Error handling:**
- If PR already merged: skip silently
- If merge conflicts: **STOP the batch** and report which plan caused it
- Do NOT merge homolog→main here — that happens once at the end

### 3E: Context Management

```bash
echo "🤖 [auto-merge:3e] checking context usage"
```

After each plan completes:
- If context > 60%: compact context before next plan
- If context > 85%: force compact — next plan needs headroom
- Log context state for debugging

**Compaction logic:**
```bash
# Drop verbose execution logs, worker output history
# Keep essential state: current plan, recent commits, PR info
# Use internal optimization — user doesn't see a prompt
```

### 3F: Update Progress and Write Batch State

```bash
echo "🤖 [auto-merge:3f] plan $PLAN_NUM complete ($CURRENT/$TOTAL)"
```

Write `.planning/.batch-progress.json` (enables crash recovery and external monitoring):

```json
{
  "pipeline": "Plans NNNN to MMMM",
  "current": 3,
  "total": 7,
  "status": "executing",
  "plans": [
    {"num": "0034", "title": "Dead code cleanup", "status": "merged", "pr": 46},
    {"num": "0035", "title": "Performance optimization", "status": "merged", "pr": 47},
    {"num": "0036", "title": "Security hardening", "status": "executing", "pr": null},
    {"num": "0037", "title": "API versioning", "status": "queued", "pr": null}
  ]
}
```

Valid plan statuses: `queued`, `executing`, `merged`, `pr-open`, `failed`, `skipped`

Update at the start and end of each plan execution. On crash recovery, read this file to determine resume point.

## Step 4: Final Merge and Report

```bash
echo "🤖 [auto-merge:4] batch pipeline complete — creating final PR"
```

### 4A: Create homolog→main PR

After ALL plans have been executed and merged into homolog, create a single PR from homolog to main with all plan summaries:

```bash
# Check if homolog has changes ahead of main
DIFF_COUNT=$(git rev-list --count origin/main..origin/homolog)
if [ "$DIFF_COUNT" -gt 0 ]; then
  MAIN_PR=$(gh pr create --base main --head homolog \
    --title "🔀 merge: batch pipeline plans NNNN-MMMM into main" \
    --body "## Batch Pipeline — Plans NNNN to MMMM

### Plans included
- Plan NNNN: description — PR #XX ✅
- Plan NNNN+1: description — PR #XX ✅
- ...

### Summary
- **Plans executed:** X
- **Plans skipped:** Y (already completed)
- **Total PRs merged to homolog:** Z

### Next steps
1. Review this PR
2. Run \`ptp\` to verify full test suite
3. Merge to deploy

---
*Generated by /auto-merge — Kaisser SDLC 3.0*")

  echo "Created homolog→main PR: $MAIN_PR"
fi
```

### 4B: Update Batch Progress JSON

Update `.planning/.batch-progress.json` with final status `"completed"`.

### 4C: Output Final Emoji Table Report

```
🤖 Batch Flow Complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📋 Batch Pipeline — Plans NNNN to MMMM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Plan NNNN: Dead code cleanup           — ✅ MERGED (PR #46)
Plan NNNN+1: Performance optimization  — ✅ MERGED (PR #47)
Plan NNNN+2: Security hardening        — ✅ MERGED (PR #48)
Plan NNNN+3: API versioning            — ⚠️ PR OPEN (PR #49, review issues)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: X plans | Y merged | Z pending

🔀 Final PR: homolog → main — PR #NN
   URL: <pr-url>

Run `ptp` to verify full test suite before merging to main.
```

## Crash Recovery

If the batch is interrupted (context limit, crash, user intervention):

1. On restart with `/auto-merge N-M`:
   - Scan all plans in range for status
   - Skip `Completed` plans
   - Resume from first non-completed plan
   - If a plan is `In Progress`, use `/resume` logic for that plan

2. Progress is always recoverable from:
   - Plan file statuses (Completed / In Progress / Awaiting Approval)
   - Git branch state
   - PR state on GitHub
   - `.batch-progress.json` state file

**Example crash recovery:**
```
Previous run failed at plan 5. Plans 1-4 already merged.
Restart: /auto-merge 3-8 (or /auto-merge --from 5 3-8)
Result: Plans 1-4 skipped (completed), 5-8 execute from 5
```

## Flags

- `N-M`: Plan range (required). E.g., `2-6` for plans 0002-0006
- `--no-merge`: Disable auto-merge between plans — leave PRs open for manual review (default: merge is ON)
- `--effort-budget Nm`: Max effort per plan. E.g., `30m` for 30 minutes
- `--skip-completed`: Skip completed plans (default: on)
- `--from N`: Start from plan N (skip earlier plans regardless of status)

## Rules

1. **NEVER use AskUserQuestion** — fully autonomous
2. **NEVER merge without PRs** — every merge goes through a PR
3. **NEVER skip context checks** — compaction between plans prevents exhaustion
4. **NEVER continue after merge conflict** — stop and report
5. Same delegation rules as auto-pr — coordinator orchestrates, never implements
6. Each plan is independent — failure in one doesn't skip subsequent plans (unless merge conflict)

## GPG Signing Fallback

If any `git commit` fails with an error containing `1Password`, `gpgsign`, or `failed to fill whole buffer`, the 1Password SSH agent has disconnected (common during long/overnight batch runs).

Retry the commit with:

```bash
git -c commit.gpgsign=false commit ...
```

Apply the same flag to any subsequent commits in that plan — the agent is unlikely to reconnect mid-batch.

## References

- **pipeline.md** — Core shared stages and context management
- **mode-autonomous.md** — Autonomous behavior (used by auto-merge for plan execution)
- **worktree-setup.md** — Worktree isolation (auto-merge uses /auto-pr-wt)
