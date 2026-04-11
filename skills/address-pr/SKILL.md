---
name: address-pr
description: >
  Fetch PR review comments, categorize feedback, implement fixes, and push.
  Use this skill whenever the user says "/address-pr", "address review", "fix review comments",
  "handle PR feedback", or any request to implement fixes based on PR review feedback.
  Also triggers on "fix the review", "address feedback", "implement review changes",
  "implement the requested changes", or "handle review comments". Fetches real comments via bravros pr-review.
---

# Address PR: Implement Review Fixes

Address PR review feedback — fetch review comments, categorize, ask user what to fix, implement, and push.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish
```

## Critical Rules

- You MUST read the actual review comments at Step 2. NEVER fabricate feedback.
- You MUST read affected files before modifying them. NEVER edit blind.
- You MUST use `AskUserQuestion` at Step 4 to confirm approach.
- You MUST run tests after fixes. NEVER skip testing.
- Follow steps 1–8 in order. Echo the step marker at the start of each step.

## Step 1/8: Get PR Number

```bash
echo "🔀 address-pr [1/8] fetching PR number"
PR_NUMBER="${ARGUMENTS:-$(gh pr view --json number -q .number)}"
```

## Step 2/8: Fetch Review Data ⚠️ MANDATORY

```bash
echo "🔀 address-pr [2/8] reading review comments"
~/.claude/bin/bravros pr-review "$PR_NUMBER" --latest
```

⛔ DO NOT fabricate review feedback. DO NOT proceed without reading real comments.
⛔ DO NOT check `gh run list` or workflow status. The review comments live on the PR, not in CI run results. **Only** use `bravros pr-review` to fetch them.

If the command returns no comments: use `AskUserQuestion` to inform the user ("No review comments found for PR #XX — nothing to address. Want to run /review first, or provide a PR number?") and STOP.

## Step 3/8: Detect Already-Fixed & Stale Reviews

```bash
echo "🔀 address-pr [3/8] detecting already-fixed and stale items"
```

### Already-fixed detection

Extract the review timestamp from the review data. Then for each file mentioned in review comments:

```bash
git log --oneline --after="<review_timestamp>" -- <file_path>
```

- If a file has commits after the review timestamp → flag as **"potentially already fixed"**
- Items with no file path (general comments, questions) → leave in normal list

### Stale review detection

```bash
LAST_FIX_COMMIT=$(git log --oneline --all --grep="address PR #" | head -1)

if [ -n "$LAST_FIX_COMMIT" ]; then
  LAST_FIX_TIME=$(git log -1 --format="%aI" $(echo "$LAST_FIX_COMMIT" | awk '{print $1}'))
  echo "Last address-pr fix: $LAST_FIX_TIME"
  # If the review is OLDER than the last fix commit → stale
fi
```

If the review is stale (older than last fix commit):
- **STOP** — use `AskUserQuestion`: "⚠️ No new review posted since your last fix (committed at $LAST_FIX_TIME). The re-review may still be running."
  - Option 1: "Wait and retry"
  - Option 2: "Force re-analyze"

If no previous fix commit or review is newer → continue.

## Step 4/8: Read Affected Files ⚠️ MANDATORY

```bash
echo "🔀 address-pr [4/8] reading affected files"
```

For each file mentioned in review comments, **READ the current code**.

⛔ DO NOT modify any file you haven't read first.

## Step 5/8: Implement ALL Fixes

```bash
echo "🔀 address-pr [5/8] implementing all fixes"
```

**No pre-approval required — implement everything.** Fix all items from the review in priority order:

1. Read the affected file(s) for each fix
2. **Grep for ALL related occurrences** of the pattern/symbol being fixed: `grep -n "<pattern>" <file>`. Never rely on `replace_all` for patterns with argument or context variations. For security fixes, every occurrence must be independently verified and patched.
3. Fix blockers → code issues → style fixes → suggestions (in priority order)
4. Run targeted tests after each fix category

⛔ **Only modify files mentioned in review comments.** No unrelated changes.

For reviewer **questions**: post a reply comment on the PR using `gh api`, then note it in the summary.

If `.planning/` has a matching plan: add "PR Review Fixes" section, mark fixes `[x]` with timestamp.

## Step 6/8: Commit & Push

```bash
echo "🔀 address-pr [6/8] committing and pushing fixes"
```

Use `/ship` with: `🐛 fix: address PR #XX review feedback` (include actual PR number)

After push, verify PR checks:
```bash
gh pr checks "$PR_NUMBER" --watch --fail-fast 2>/dev/null || gh pr checks "$PR_NUMBER"
```

## Step 7/8: Report Fixes

```bash
echo "🔀 address-pr [7/8] reporting what was fixed"
```

```
PR #XX Fix Summary:
  ✅ Already fixed: N items (committed after review)
     - <file>: <brief description of comment>
  ✅ Fixed now:
     - Blockers: N items fixed
     - Code issues: N items fixed
     - Style fixes: N items fixed
     - Suggestions: N items fixed
     - Questions: N items responded
  Total: N items addressed
```

## Step 8/8: Smart Next Action ⚠️ MANDATORY

```bash
echo "🔀 address-pr [8/8] smart next action"
```

### Severity Matrix — check BOTH fix type AND file sensitivity

**Re-review RECOMMENDED** (⚠️) when any of:
- Blockers were fixed (logic, security, validation)
- Files were significantly restructured
- Business logic or control flow changed
- Test behavior was modified (not just added)
- Security-sensitive files were modified (auth, payments, permissions)

**Re-review optional** (✅) when ALL fixes were:
- Style/formatting only
- Typo or comment fixes
- Simple additions (return types, null checks)
- Test additions with no production code changes

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: fixes-pushed. NEXT: review"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**If lock file present (autonomous mode):** Output `STATUS: fixes-pushed. NEXT: review` and return.

**If no lock file:**

⛔ **STOP. Use `AskUserQuestion`:**
- **Question:** "All review fixes pushed. [⚠️ Re-review recommended / ✅ Re-review likely unnecessary]. What's next?"
- **Option 1:** "Trigger re-review" → delegate to `/review`
- **Option 2:** "Finish & merge" → run `/finish`
- **Option 3:** "Done for now"

Use $ARGUMENTS as PR number if provided.
