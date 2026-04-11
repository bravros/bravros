---
name: finish
description: >
  Mark feature complete, merge PR, rename plan file, and handle homolog→main flow.
  Use this skill whenever the user says "/finish", "finish this", "merge and finish",
  "wrap up", "close this out", or any request to complete a feature and merge the PR.
  Also triggers on "merge PR", "finish the feature", "we're done", "mark as complete",
  "the PR is approved let's merge", or "feature is complete".
  Run AFTER PR is approved. Handles plan rename and main merge.
---

# Finish: Complete Feature and Merge

Mark the current feature as complete and merge to base branch (and optionally to main).

Run AFTER PR is approved.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish
```

## Critical Rules

- You MUST use `AskUserQuestion` at Step 7/7. ALWAYS ask about merging to main. NEVER skip it.
- Step 6/7 runs BEFORE Step 7/7. In normal mode, use `AskUserQuestion` for deploy. In --batch mode, auto-deploy without asking.
- Step 2/7 delegates to `bravros finish` — NEVER fabricate plan details or skip the CLI call.
- Follow steps in order. DO NOT skip or reorder steps.
- Do NOT modify application code — finish is a git/project management operation only.
- You MUST emit the checkpoint echo (`echo "🏁 [finish] (N/7) ..."`) for EVERY step you execute, even when resuming. The audit hook uses these to detect skill context. Skipping the echo WILL cause merge commands to be blocked.

## Step 0: Verify PR is Approved

Parse $ARGUMENTS for:
- `--batch` → batch mode (simplified flow, no user prompts)

```bash
PR_STATE=$(gh pr view --json reviewDecision --jq '.reviewDecision' 2>/dev/null)
PR_MERGEABLE=$(gh pr view --json mergeable --jq '.mergeable' 2>/dev/null)
```

If `reviewDecision` is not `APPROVED` and the PR has required reviews, warn the user:
"PR is not yet approved. Are you sure you want to merge?"

Only proceed if the user confirms or the PR has no required review policy.

## Batch Mode Shortcut

When `--batch` is set, execute a simplified flow:
- Skip Step 3/7 (CI tests — auto-merge runs these at the end)
- Skip Step 7/7 (main merge AskUserQuestion — auto-merge handles homolog→main at the end)
- Execute only: Step 1/7 (base branch) → Step 2/7 (complete plan via CLI) → Step 4/7 (conflict check) → Step 5/7 (merge PR + cleanup) → Step 6/7 (auto-deploy if project: claude) → Step 8 (done)
- NEVER use AskUserQuestion in batch mode

## Step 1/7: Determine Base Branch

Run: `echo "🏁 [finish] (1/7) determining base branch"`

```bash
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" = "homolog" ]; then
    BASE_BRANCH="main"
    HAS_HOMOLOG=false
elif git show-ref --verify --quiet refs/heads/homolog || git show-ref --verify --quiet refs/remotes/origin/homolog; then
    BASE_BRANCH="homolog"
    HAS_HOMOLOG=true
else
    BASE_BRANCH="main"
    HAS_HOMOLOG=false
fi
```

## Step 2/7: Complete Plan via CLI

Run: `echo "🏁 [finish] (2/7) completing plan via CLI"`

Find the PR number, ensure the plan file has it, then delegate all plan completion to the CLI:

```bash
# Try current branch PR, then fall back to frontmatter pr: field
PR_NUMBER=$(gh pr view --json number --jq '.number' 2>/dev/null)
if [ -z "$PR_NUMBER" ] || [ "$PR_NUMBER" = "null" ]; then
    # Extract from frontmatter — handle both "pr: 1" and "pr: https://...pull/1"
    PR_RAW=$(grep '^pr:' "$PLAN_FILE" | awk '{print $2}')
    PR_NUMBER=$(echo "$PR_RAW" | grep -oE '[0-9]+$')
fi

# Ensure the plan file has the PR number before calling finish.
# If pr: is null/empty, sdlc finish won't find the plan — sync it first.
PLAN_PR=$(grep '^pr:' .planning/*-todo.md 2>/dev/null | head -1 | awk '{print $2}')
if [ "$PLAN_PR" = "null" ] || [ -z "$PLAN_PR" ]; then
    ~/.claude/bin/bravros sync --pr "$PR_NUMBER"
fi

# Run the finish command — handles mark-complete, backlog archive, rename, stage, and commit
~/.claude/bin/bravros finish --pr "$PR_NUMBER"

# Push the commit produced by the CLI
git push
```

This single command handles everything atomically:
- Marks the plan as complete (`sdlc sync --finish`)
- Archives the backlog item if a `backlog:` field exists in the plan frontmatter
- Renames the plan file from `-todo.md` to `-complete.md` (`git mv`)
- Stages and commits all `.planning/` changes

To preview what would happen without committing, run:
```bash
~/.claude/bin/bravros finish --pr "$PR_NUMBER" --dry-run
```

## Step 3/7: Trigger CI Tests Before Merge

Run: `echo "🏁 [finish] (3/7) checking CI tests"`

Use the CLI to detect CI workflow status in a single call:

```bash
HAS_CI=$(~/.claude/bin/bravros ci-check --field has_ci 2>/dev/null)
RELEVANT=$(~/.claude/bin/bravros ci-check --field relevant 2>/dev/null)
```

**If `has_ci` is false or `relevant` is false → skip silently.** Not all projects have active CI.

If CI is relevant (`has_ci=True` and `relevant=True`), trigger and wait:

```bash
# 1. Post @tests comment to trigger the workflow
gh pr comment "$PR_NUMBER" --body "@tests"

# 2. Wait for the workflow to start (15s — it takes a moment to queue)
sleep 15

# 3. Get the latest run ID for the tests workflow
RUN_ID=$(gh run list --workflow=tests.yml --limit=1 --json databaseId -q '.[0].databaseId')

# 4. Watch the run until it completes
if [ -n "$RUN_ID" ] && [ "$RUN_ID" != "null" ]; then
    gh run watch "$RUN_ID" --exit-status
    RESULT=$?
    if [ $RESULT -ne 0 ]; then
        echo "❌ CI tests failed — cannot merge"
        # STOP. Use AskUserQuestion:
        # "CI tests failed on PR #XX. What do you want to do?"
        # Option 1: "Fix and retry" — investigate failures
        # Option 2: "Merge anyway" — skip CI gate
        # Option 3: "Abort" — don't merge
    else
        echo "✅ CI tests passed"
    fi
fi
```

## Step 4/7: Check for Merge Conflicts

```bash
echo "🏁 [finish] (4/7) checking for merge conflicts"
```

Pre-flight check before merge. The CLI handles this internally, but the echo is required for the audit hook to detect skill context.

## Step 5/7: Merge PR and Clean Up Branch

```bash
echo "🏁 [finish] (5/7) merging PR and cleaning up branch"
```

⚠️ **CRITICAL:** Both Step 4/7 and 5/7 echos MUST run BEFORE any merge operations, even when resuming. Skipping them breaks audit hook context detection.

### Pre-merge Commit Capture

Before merging, capture the feature branch state for post-merge verification:

```bash
# Capture feature branch tip before merge (for post-merge verification)
PRE_MERGE_COMMIT=$(git rev-parse HEAD)
# NOTE: Use :(exclude) long form, NOT :! shorthand — :! breaks on dirs with underscores
# (e.g., :!__tests__/ → "fatal: Unimplemented pathspec magic '_'")
FEATURE_FILES=$(git diff --name-only "origin/$BASE_BRANCH"..."$PRE_MERGE_COMMIT" -- '*.php' '*.ts' '*.tsx' '*.js' '*.jsx' '*.py' '*.go' ':(exclude)tests/' ':(exclude)test/' ':(exclude)spec/' ':(exclude)__tests__/')
```

Use `sdlc merge-pr` — it handles conflict check, merge, and branch cleanup atomically:

```bash
bravros merge-pr "$PR_NUMBER" --auto-resolve-planning
```

- `--auto-resolve-planning`: auto-resolves `.planning/` conflicts (takes base branch version). If code conflicts exist, it stops and reports them.
- `--delete-branch` is on by default. The CLI reads `.skaisser.yml` for `staging_branch` and never deletes permanent branches (main, homolog, staging, develop).
- Returns JSON: `{"pr", "state", "branch_deleted", "conflicts_resolved"}`

If merge fails due to code conflicts:
```
❌ Code conflicts detected — manual resolution needed
```
STOP. Use `AskUserQuestion` to inform the user and ask how to proceed. Do NOT continue if merge failed.

After confirmed merge — switch to base branch locally:
```bash
git checkout "$BASE_BRANCH"
git fetch origin "$BASE_BRANCH"
git reset --hard "origin/$BASE_BRANCH"  # Safe: merge just succeeded, remote is authoritative. Avoids divergent-branch errors from squash merges.
git branch -d "$FEATURE_BRANCH" 2>/dev/null || true  # local cleanup if not already removed
```

### Post-Merge Verification

After branch cleanup, verify that the squash merge preserved all feature changes:

```bash
# Verify feature changes survived the merge
echo "🔍 Verifying merge preserved feature changes..."
LOST_FILES=""
for FILE in $FEATURE_FILES; do
  if [ -f "$FILE" ]; then
    DIFF=$(git diff "$PRE_MERGE_COMMIT" "origin/$BASE_BRANCH" -- "$FILE")
    if [ -n "$DIFF" ]; then
      LOST_FILES="$LOST_FILES\n  - $FILE"
    fi
  else
    LOST_FILES="$LOST_FILES\n  - $FILE (file missing from merged result)"
  fi
done

if [ -n "$LOST_FILES" ]; then
  echo "⚠️ WARNING: The following files show unexpected diff after merge — changes may have been lost:"
  printf '%b\n' "$LOST_FILES"
  
  # Branch on execution mode
  if [ "$BATCH_MODE" = "true" ]; then
    # In batch mode: log warning and STOP execution
    echo "❌ [finish] Post-merge verification failed — do NOT proceed to Step 6/7"
    echo "Batch mode cannot continue with potential data loss. Aborting."
    exit 1
  else
    # In normal mode: use AskUserQuestion to alert user
    # The skill code should handle: AskUserQuestion --title "Post-Merge Verification" --message "Post-merge verification found files where feature changes may have been lost: $LOST_FILES. Check these files manually before continuing."
    echo "Use AskUserQuestion: 'Post-merge verification found files where feature changes may have been lost: [list]. Check these files manually before continuing.'"
  fi
else
  echo "✅ All feature files verified — merge preserved expected changes"
fi
```

**Behavior by mode:**

- **Normal mode:** Use `AskUserQuestion` to alert the user: "Post-merge verification found files where feature changes may have been lost: {list}. Check these files manually before continuing." Stop and wait for user action.
- **Batch mode (`--batch`):** Log the warning and STOP execution — do NOT proceed to Step 6/7. Return an error (exit 1) so the calling skill (e.g., `/auto-merge`) can handle it gracefully.

If all files show expected diffs, proceed to Step 6/7 normally.

## Step 6/7: Auto-deploy to ~/.claude/ (claude config repo only)

Run: `echo "🏁 [finish] (6/7) checking for auto-deploy"`

Detect if this is the claude config repo:

```bash
PROJECT=$(~/.claude/bin/bravros meta 2>/dev/null | grep '^project:' | awk '{print $2}')
```

Only run this step if `PROJECT` equals `claude`. Otherwise skip silently.

### Deploy Mapping (repo → ~/.claude/)

```bash
~/.claude/bin/bravros deploy
```

**In normal mode:** Use `AskUserQuestion` — "Deploy changes to ~/.claude/? ($DEPLOY_COUNT files changed)"
- **Yes, deploy** → run deploy block above
- **No, skip** → skip silently

**In --batch mode:** Auto-deploy without asking (no AskUserQuestion).

## Step 7/7: Handle Main Branch ⚠️ MANDATORY

Run: `echo "🏁 [finish] (7/7) asking user about main merge"`

> **Lock File Note:** ALWAYS use `AskUserQuestion` regardless of `.planning/.auto-pr-lock`. Merge to main is sacred — auto-pr stops here with a recommendation, never merges. The lock file has NO effect on this step.

⛔ **STOP. You MUST use `AskUserQuestion` tool here. ALWAYS.**

If `HAS_HOMOLOG` is true (merged to homolog):
- **Question:** "PR merged to homolog. What about main?"
- **Option 1:** "Merge to main now" — Create PR and merge (see commands below)
- **Option 2:** "Create PR, I'll merge manually" — Create PR only
- **Option 3:** "I'll do it later"

**Merge to main commands (use `gh pr`, NEVER `gh api`):**

```bash
# Check for existing PR first
EXISTING_PR=$(gh pr list --base main --head homolog --json number,state --jq '.[0]' 2>/dev/null)
if [ -n "$EXISTING_PR" ] && [ "$EXISTING_PR" != "null" ]; then
    PR_NUM=$(echo "$EXISTING_PR" | jq -r '.number')
    # Use AskUserQuestion: "PR #$PR_NUM already exists for homolog→main. Merge it or create new?"
fi

# Create PR from homolog → main (if no existing PR)
gh pr create --base main --head homolog \
  --title "<emoji> <type>: <description>" \
  --body "Merges homolog → main. Contains PR #<NUMBER>: <brief>"

# Merge
MAIN_PR=$(gh pr list --base main --head homolog --json number --jq '.[0].number')
bravros merge-pr "$MAIN_PR" --merge-strategy merge --auto-resolve-planning
```

### Quick-resolve: Test file conflicts only

If ALL conflicts are in `tests/` directory (add/add conflicts from squash merges):

```bash
# Check if all conflicts are test files
CONFLICT_FILES=$(git diff --name-only --diff-filter=U)
NON_TEST_CONFLICTS=$(echo "$CONFLICT_FILES" | grep -v '^tests/' | head -1)
if [ -z "$NON_TEST_CONFLICTS" ]; then
  # All conflicts are in tests/ — safe to auto-resolve with homolog version
  git checkout --theirs -- tests/
  git add tests/
  echo "✅ Auto-resolved test file conflicts (took homolog version)"
else
  # Application code conflicts — use full merge branch resolution below
  echo "⚠️ Application code conflicts detected — using merge branch resolution"
fi
```

This avoids the full merge-branch workflow for the common case where only test files conflict (add/add from squash merges). Only prompt for application code conflicts.

### If homolog→main merge has conflicts

⛔ **NEVER** try `git push origin main` directly — the audit hook blocks it.
⛔ **NEVER** use `git reset --hard` to undo a failed merge.

When `merge-pr` fails with conflicts, use a **merge branch**:

```bash
# 1. Close the conflicting PR
gh pr close "$MAIN_PR" --comment "Conflicts detected — resolving via merge branch."

# 2. Create merge branch from main
git fetch origin main
git checkout -b merge/homolog-to-main origin/main

# 3. Merge homolog into it (will have conflicts)
git merge origin/homolog -m "🔀 merge: homolog into main" || true

# 4. Resolve conflicts — take homolog version (latest code)
CONFLICTED=$(git diff --name-only --diff-filter=U)
git checkout --theirs $CONFLICTED
git add $CONFLICTED
git commit -m "🔀 merge: homolog into main — conflicts resolved (took homolog)"

# 5. Push merge branch and create PR
git push -u origin merge/homolog-to-main
gh pr create --base main --head merge/homolog-to-main \
  --title "🔀 merge: homolog into main" \
  --body "Merges homolog → main with conflict resolution. Took homolog version for conflicting files."

# 6. Merge the PR
MERGE_PR=$(gh pr view --json number --jq '.number')
bravros merge-pr "$MERGE_PR" --merge-strategy merge

# 7. Cleanup
git checkout homolog
git branch -D merge/homolog-to-main 2>/dev/null || true
```

If conflicts are in **application code** (not just .planning/), use `AskUserQuestion` before auto-resolving — the user may want to review which version to keep.

If `HAS_HOMOLOG` is false (merged directly to main):
- **Question:** "PR merged to main. Anything else needed?"
- **Option 1:** "All done"
- **Option 2:** "Deploy / run migrations"

## Step 7.5: Clean Orphan Plan Files

After any main↔homolog sync (merging main into homolog or vice versa), squash-merge loses `git mv` rename history. This causes ghost `-todo.md` files to reappear alongside their `-complete.md` counterparts.

**Always run after syncing branches:**

```bash
~/.claude/bin/bravros clean-todos
```

If files were removed, stage and commit:
```bash
git add .planning/
git diff --cached --quiet || git commit -m "🧹 chore: remove orphan plan todo files after branch sync"
git push
```

## Step 8: Done

Report merges, branch cleanup, and post-deploy reminders.
If worktree: remind to run `/complete` from main repo terminal for cleanup.

## Flags

- `--batch`: Simplified batch mode — skip CI, skip main merge prompt. Used by `/auto-merge` for sequential plan execution.

Use $ARGUMENTS for any additional context.