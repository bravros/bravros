---
name: review
description: >
  Trigger @claude PR review on a Pull Request via GitHub Actions.
  Use this skill whenever the user says "/review", "review the PR", "code review",
  "trigger review", or any request to get an automated code review on a PR.
  Also triggers on "claude review", "run review", or "check the PR".
  Posts a comment mentioning @claude which triggers the GitHub Action.
---

# Review: Trigger PR Code Review

Trigger mention-based PR review on a Pull Request.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

⛔ **CRITICAL GUARDRAILS:**
- Do NOT perform the review yourself — this skill only posts a comment to trigger the CI-based review
- Do NOT modify any code files, project files, or configuration
- Do NOT merge, approve, or close the PR
- Do NOT run tests or make any changes to the codebase

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish
```

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/3: Get PR Number and Trigger Review

```bash
echo "🔀 review [1/3] triggering PR review"
```

- If `$ARGUMENTS` provided, use it as PR number
- Otherwise: `gh pr view --json number -q .number`

If no PR found, STOP: "No open PR found. Create one with /pr first."

Then trigger the review:

```bash
gh pr comment <PR_NUMBER> --body "@claude review this PR and check if we are able to merge. Analyze the code changes for any issues, security concerns, or improvements needed."
```

If the comment fails (non-zero exit), STOP and report the error.

## Step 2/3: Verify Action Triggered

```bash
echo "🔀 review [2/3] waiting for action and polling status"
```

Check that the GitHub Action workflow was triggered:

```bash
gh run list --workflow=claude.yml --limit=1 --json status,createdAt,event -q '.[0]'
```

Report the run status. If no run found, note that the action may take a moment to start or may not be configured.

## Step 3/3: Confirm and Ask

```bash
echo "🔀 review [3/3] reporting results and next step"
```

Confirm review was triggered and provide the PR link. Review usually appears within 2-5 minutes.

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: review-result. NEXT: address-pr or finish"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**If lock file present (autonomous mode):** Output `STATUS: review-result. NEXT: address-pr or finish` and return.

**If no lock file:**

⛔ **STOP. You MUST use `AskUserQuestion` tool here.**

- **Question:** "Review triggered on PR #N. Ready to address feedback?"
- **Option 1:** "Address review now" — Run /address-pr
- **Option 2:** "I'll check the review first"

Use $ARGUMENTS as PR number if provided.