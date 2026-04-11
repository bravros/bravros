---
name: pr
description: >
  Create a Pull Request with plan context and proper base branch detection.
  Use this skill whenever the user says "/pr", "create a PR", "open a pull request",
  "make a PR", "create pull request", or any request to create a PR for the current branch.
  Also triggers on "open PR", "submit PR", "PR for this branch", "push and create PR",
  "I'm ready for a PR", or "let's open a pull request for this".
  ALWAYS runs /ship first and detects the correct base branch (feature→homolog→main flow).
---

# PR: Create Pull Request

Create a Pull Request to base branch with descriptive summary.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish
```

## Critical Rules

- You MUST run `/ship` at Step 2/6. NEVER create a PR with uncommitted changes.
- You MUST detect the correct base branch. PRs NEVER target `main` directly (unless from homolog).
- Follow steps in order. DO NOT skip or reorder steps.
- **PR title MUST be under 70 characters.** Use the body for details, not the title.
- **NEVER add AI signatures** to PR title or body. No "Generated with Claude Code", no "Co-Authored-By", no "🤖 Generated", no AI attribution of any kind. The audit hook will BLOCK this.
- Do NOT run tests — CI handles that.
- Do NOT modify application code — PR is a git/GitHub operation only.
- Do NOT re-read the entire codebase — summarize from commits and plan context.

### Auto-mode detection

If `$ARGUMENTS` contains `--auto`:
- Set `AUTO_MODE = true`
- Strip `--auto` from `$ARGUMENTS`
- All `AskUserQuestion` calls below become no-ops (skip and return)

## Step 1/6: Plan Check Gate

```bash
echo "🔀 pr [1/6] checking plan gate"
~/.claude/bin/bravros meta
```

Use `plan_file` from JSON output to check if a plan exists. If a plan file exists, check if `/plan-check` was run:
```bash
PLAN_NUM=$(echo "$PLAN_FILE" | grep -oE '[0-9]{4}')
CHECKED=$(~/.claude/bin/bravros plan-check-status --field checked 2>/dev/null)
```

- **Plan exists + NOT checked** → ⛔ STOP. Tell the user: "Run `/plan-check` first."
- **Plan exists + checked** → Continue.
- **No plan file** → This was a `/quick` task. Continue.

## Step 2/6: Ship Changes ⚠️ MANDATORY

Run: `echo "🔀 pr [2/6] shipping changes before PR"`

⛔ **Run `/ship` first to commit and push all current changes.** DO NOT skip this.

## Step 3/6: Determine Base Branch

**CRITICAL: PRs NEVER target `main` directly. Always go through `homolog` first.**

```bash
echo "🔀 pr [3/6] determining base branch"
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" = "homolog" ]; then
    BASE_BRANCH="main"
elif git show-ref --verify --quiet refs/heads/homolog || git show-ref --verify --quiet refs/remotes/origin/homolog; then
    BASE_BRANCH="homolog"
else
    BASE_BRANCH="main"
fi
```

```bash
# Check if feature branch is behind base
git fetch origin "$BASE_BRANCH" --quiet
BEHIND=$(git rev-list --count HEAD..origin/"$BASE_BRANCH" 2>/dev/null || echo "0")
if [ "$BEHIND" -gt 0 ]; then
    echo "⚠️ Branch is $BEHIND commits behind $BASE_BRANCH"
    if [ "$AUTO_MODE" = "true" ] || [ -f ".planning/.auto-pr-lock" ]; then
        echo "🔄 Auto-rebasing onto origin/$BASE_BRANCH..."
        git rebase origin/"$BASE_BRANCH"
        if [ $? -ne 0 ]; then
            echo "❌ Rebase failed with conflicts. Aborting rebase."
            git rebase --abort
            echo "⚠️ Proceeding without rebase — PR may contain stale changes."
        else
            echo "✅ Rebase succeeded — pushing updated branch..."
            git push --force-with-lease origin HEAD
        fi
    else
        # Interactive mode — ask user
        # Use AskUserQuestion: "Branch is $BEHIND commits behind $BASE_BRANCH. Rebase now?"
        # Option 1: "Rebase now" — run git rebase origin/$BASE_BRANCH
        # Option 2: "Proceed without rebase" — continue as-is
    fi
fi
```

**Flow: `feature/* → homolog → main`**

## Step 4/6: Gather Context

```bash
echo "🔀 pr [4/6] gathering context"
~/.claude/bin/bravros context "$BASE_BRANCH"
```

Read plan file from `.planning/` if exists for PR context.

## Step 5/6: Create PR

Run: `echo "🔀 pr [5/6] creating pull request"`

Title format: `<emoji> <type>: <description>` — **MUST be under 70 characters total.**

Create the PR and capture the URL:

```bash
PR_URL=$(gh pr create --base "$BASE_BRANCH" --title "<emoji> <type>: <title>" --body "$(cat <<'EOF'
## Summary
[What and why — 1-3 bullet points]

## Changes
- [Change 1]
- [Change 2]

## Technical Notes
[Important details, patterns, decisions]

## Test Plan
- [ ] [How to verify change 1]
- [ ] [How to verify change 2]

## References
[Related issues or PRs, e.g. Closes #123]
EOF
)")
echo "$PR_URL"

# Link PR number to plan frontmatter
PR_NUMBER=$(echo "$PR_URL" | grep -oE '[0-9]+$')
PLAN_FILE=$(ls .planning/*-todo.md 2>/dev/null | head -1)
if [ -n "$PLAN_FILE" ] && [ -n "$PR_NUMBER" ]; then
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s/^pr: null/pr: $PR_NUMBER/" "$PLAN_FILE"
    else
        sed -i "s/^pr: null/pr: $PR_NUMBER/" "$PLAN_FILE"
    fi
    git add "$PLAN_FILE"
    git commit -m "📋 plan: link PR #$PR_NUMBER to plan frontmatter"
    git push
fi
```

Display the PR URL to the user after creation.

## Step 6/6: After Creation

Run: `echo "🔀 pr [6/6] post-creation options"`

**If NOT `--auto` mode:**

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: pr-created. PR: #$PR_NUMBER. NEXT: review"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**If lock file present (autonomous mode):** Output `STATUS: pr-created. PR: #N. NEXT: review` and return.

**If no lock file:**

⛔ **MANDATORY: Use the `AskUserQuestion` tool.** Do NOT output options as plain text.
This is a BLOCKING requirement — the skill is NOT complete until AskUserQuestion is called.

**Exception:** In autonomous/batch mode (`--auto` or `--batch` flag active), output options as plain text instead of using AskUserQuestion — autonomous pipelines must not block on user input.

- **Question:** "PR created. What's next?"
- **Option 1:** "Run /review" — Trigger @claude code review on the PR
- **Option 2:** "I'll handle review manually"

**If `--auto` mode:** Output the PR URL and return immediately. Do not prompt.

## Flags

- `--auto`: Suppress all `AskUserQuestion` checkpoints. Used by `/auto-pr` and `/auto-pr-wt` when delegating to this skill. When `--auto` is present, skip the final "What's next?" prompt and return immediately after completing work.

Use $ARGUMENTS for any additional context.