---
name: pr-review
core: true
description: Post @claude review comment on the current PR and ask what's next. Use on `/pr-review` to trigger the GitHub Actions review workflow.
---

# pr-review

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: post ONE verbatim `@claude` comment; the GitHub Action reviews asynchronously (~2–5 min)
and posts back to the PR. This skill never reviews, never polls, never merges.

## Core Steps

1. **Determine PR Number**: Use `$ARGUMENTS` if numeric, else `gh pr view --json number -q .number`. If none, STOP ("create one with /pr first").
2. **Branch Sync**: If behind base branch, rebase and `git push --force-with-lease` first. Handle conflicts according to mode (ask in interactive / note & proceed in autonomous).
3. **Post Comment**: Send verbatim `@claude` comment with visible sentinel verdict lines (`BRAVROS-VERDICT: approved` / `BRAVROS-VERDICT: changes-requested`).
   - **NEVER write a bare `#N` for a review-finding number.** GitHub autolinks `#N` in every
     issue/PR body — it cannot be disabled, and it also writes a cross-reference event onto that
     issue's timeline, so referring to "finding #3" silently spams an unrelated old issue and
     renders as its title mid-sentence. Write `finding 3`, or wrap it in backticks. Reserve bare
     `#N` for a genuine issue/PR you mean to link. Same rule applies to the PR body.
4. **Verdict & Stamp Rules**:
   - `BRAVROS-VERDICT:` is authoritative. Prose is report-only.
   - `bravros pr-review "$PR" --write-stamp` is the single source of truth for writing `.planning/.review-stamp-<PR>.json`.
5. **After Posting**:
   - Autonomous: Print `STATUS: review-triggered. NEXT: wait for stamp`.
   - Interactive: Advise user to run `/address-pr` when complete.
