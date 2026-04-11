---
name: complete
description: >
  Clean up worktree after PR merge — only needed for /plan-wt flows.
  Use this skill whenever the user says "/complete", "clean up worktree", "remove worktree",
  "delete the worktree", "worktree cleanup",
  or any request to clean up after a worktree-based feature is merged.
  For standard /plan flows, /finish handles everything — /complete is a no-op.
  Detects environment automatically and skips if nothing to clean up.
---

# Complete: Worktree Cleanup

Clean up after a PR has been merged. **Only needed for worktree flows (`/plan-wt`).**

For standard `/plan` flows, `/finish` already handles everything. This command detects the environment and skips if there's nothing to clean up.

```
Standard: /plan → ... → /finish ✅ (done)
Worktree: /plan-wt → ... → /finish → /complete (run from main repo terminal)
```

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/3: Detect + Validate Environment

Single bash call to detect worktree, check PR status, and run safety checks:

```bash
echo "🔀 complete [1/3] detecting and validating environment"
IS_WORKTREE=$(git rev-parse --git-dir 2>/dev/null | grep -q "worktrees" && echo "yes" || echo "no")
CURRENT_BRANCH=$(git branch --show-current)
echo "worktree=$IS_WORKTREE branch=$CURRENT_BRANCH"
# If in worktree: check PR merged + safety
if [ "$IS_WORKTREE" = "yes" ]; then
  gh pr list --head "$CURRENT_BRANCH" --state merged --json number --jq '.[0].number' || echo "NO_MERGED_PR"
  git diff --quiet && git diff --cached --quiet && echo "clean" || echo "ERROR: uncommitted changes"
  [[ "$CURRENT_BRANCH" =~ ^(main|master|homolog|develop)$ ]] && echo "ERROR: protected branch" || echo "branch_safe"
fi
```

### If NOT in a worktree:
Check for leftover branches. If none, report "Nothing to clean up" and STOP.

### If in a worktree but PR NOT merged:
STOP. NEVER delete branches of unmerged PRs.

### If in a worktree with uncommitted changes or protected branch:
STOP with error.

### If all checks pass:
Use AskUserQuestion to confirm: "Will remove worktree at [path], delete branch [name] locally and remotely, and unlink Herd site (if applicable). Proceed?"

## Step 2/3: Execute Cleanup

```bash
echo "🔀 complete [2/3] executing cleanup"
MAIN_REPO=$(git worktree list | head -1 | awk '{print $1}')
WORKTREE_PATH=$(pwd)
command -v herd &>/dev/null && herd unsecure 2>/dev/null; command -v herd &>/dev/null && herd unlink 2>/dev/null
cd "$MAIN_REPO"
git fetch --all --prune
git checkout "$BASE_BRANCH" && git pull origin "$BASE_BRANCH"
~/.claude/bin/bravros worktree cleanup "$WORKTREE_PATH" --force --delete-remote
```

## Step 3/3: Verify + Report

```bash
echo "🔀 complete [3/3] verifying and reporting"
```

Run `git worktree list` and confirm the removed worktree path no longer appears in the output. If it still appears, run `git worktree prune` again and re-check. Only proceed to report once the worktree is confirmed gone.

```bash
REMAINING=$(git worktree list)
echo "$REMAINING"
echo "$REMAINING" | grep -q "$WORKTREE_PATH" && echo "ERROR: worktree still present" || echo "VERIFIED: worktree removed"
```

If verification fails, report the error and STOP — do not claim success.

If verified:

```
Cleanup complete!
  Removed: [worktree path], branch (local + remote), Herd site (if applicable)
  Current: [main repo path] on $BASE_BRANCH
  Remaining worktrees: [list from verification output]
```

## Safety

- Verify PR is merged before deleting branch
- Use `-d` (safe delete) when possible
- Never delete main, master, homolog, or develop
- All user interactions MUST use `AskUserQuestion` tool, never plain text questions

Use $ARGUMENTS for any additional context.