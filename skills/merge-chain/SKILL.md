---
name: merge-chain
description: >
  Sequential PR merge with conflict resolution — merge multiple PRs in order with automatic
  conflict handling. Use this skill whenever the user says "/merge-chain", "merge chain",
  "merge these PRs", "sequential merge", "merge PRs in order", "merge 46 47 48",
  or any request to merge multiple PRs sequentially. Also triggers on "merge all PRs",
  "chain merge", "merge in sequence", "merge one by one", "merge them in order",
  "merge all open PRs", "merge the chain".
---

# Merge Chain: Sequential PR Merge with Conflict Resolution

Merge multiple PRs sequentially in the correct order, handling conflicts automatically where safe.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

```
/merge-chain 46 47 48 49 [--base <branch>] [--force-resolve]
  → Validate all PRs → show merge plan → merge one by one
  → Auto-resolve .planning/ conflicts → STOP on code conflicts
  → Pull after each merge → final report
```

## Why This Exists

In batch flows, PRs must merge sequentially. Manual merging is error-prone — if PR N removes files that PR N+2 modifies, conflicts cascade. This skill automates the sequential merge with intelligent conflict handling:
- `.planning/` conflicts are auto-resolved (these are metadata, not code)
- Code conflicts STOP the chain and ask the user to decide
- Each merge pulls the latest base branch before proceeding

## Critical Rules

1. **Plan-file-only conflicts (`.planning/`) are auto-resolved** — take the base branch version.
2. **Code conflicts STOP the chain** — the user must decide how to proceed.
3. **Always pull after each merge** to keep the base branch current for the next PR.
4. **Never force-merge or skip failing CI checks** — wait for checks to pass.
5. **If a PR is already merged, skip it silently** and continue to the next.
6. **Stop on unexpected errors** — never force-delete branches or force-push without confirmation.
7. **Never skip hooks or bypass signing** — all merges go through normal git flow.
8. **Use emoji commit messages** for all merge commits (e.g., `🔀 merge: PR #46 — dead code cleanup`).

---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 0/5: Parse Arguments

```bash
echo "🔀 merge-chain [0/5] parsing arguments"
```

Parse $ARGUMENTS for:
- PR numbers: space-separated (`46 47 48`) or comma-separated (`46,47,48`) or range-like (`46-49`)
- `--base <branch>` → override base branch (default: auto-detect from first PR's base, or `homolog`, or `main`)
- `--force-resolve` → auto-resolve ALL conflicts preferring base branch (DANGEROUS — skips user confirmation for code conflicts)

Validate:
- At least 2 PR numbers provided
- All PR numbers are valid integers

## Step 1/5: Validate PRs

```bash
echo "🔀 merge-chain [1/5] validating PRs"
```

For each PR number:
```bash
gh pr view $PR_NUM --json number,title,state,headRefName,baseRefName,mergeable,statusCheckRollup
```

- If a PR doesn't exist: ERROR and stop
- If a PR is already merged: mark as SKIP
- If a PR is closed (not merged): WARN and ask user whether to include it
- Collect: PR number, title, state, head branch, base branch, mergeable status

Determine base branch:
1. If `--base` flag provided, use that
2. Otherwise, use the base branch of the first PR
3. Verify all PRs target the same base branch (warn if they don't)

## Step 2/5: Pre-flight — Show Merge Plan

```bash
echo "🔀 merge-chain [2/5] showing merge plan"
```

Display the merge plan:

```
🔗 Merge Chain — N PRs
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PR #46: Dead code cleanup           — ⏳ PENDING
PR #47: Performance optimization    — ⏳ PENDING
PR #48: Security hardening          — ⏳ PENDING
PR #49: UX polish                   — ⏳ PENDING
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Base branch: homolog
```

If any PRs are marked SKIP (already merged), show them as:
```
PR #45: Previous cleanup            — ⏭️ SKIP (already merged)
```

## Step 3/5: Sequential Merge Loop

```bash
echo "🔀 merge-chain [3/5] merging sequentially"
```

```bash
echo "🔗 [merge-chain:3] starting merge loop"
```

For each PR in order:

### 3a. Attempt Merge

Use `sdlc merge-pr` — handles conflict detection, merge, and branch cleanup atomically:

```bash
MERGE_RESULT=$(bravros merge-pr $PR_NUM --auto-resolve-planning)
```

- `--auto-resolve-planning`: auto-resolves `.planning/` conflicts (takes base branch version)
- `--delete-branch` is on by default (permanent branches are never deleted — the CLI reads `.bravros.yml`)
- Returns JSON: `{"pr", "state", "branch_deleted", "conflicts_resolved"}`

### 3b. Handle Merge Failure (Conflicts)

If `sdlc merge-pr` exits non-zero due to code conflicts:

- If `--force-resolve` flag is set: re-run with `--force-resolve` flag passed through:
  ```bash
  bravros merge-pr $PR_NUM --auto-resolve-planning --force-resolve
  ```
- Otherwise: STOP and report which files conflict. Use `AskUserQuestion`:
  - Option 1: "Resolve automatically (prefer base branch for conflicting files)" → re-run with `--force-resolve`
  - Option 2: "Stop — I'll resolve manually" → stop the chain and report progress so far

### 3c. Post-Merge Sync

After successful merge, pull the base branch to stay current for the next PR:
```bash
git checkout $BASE_BRANCH
git pull origin $BASE_BRANCH
```

### 3d. Update Progress

Update and re-display the progress table with the current PR marked as done:
```
PR #46: Dead code cleanup           — ✅ MERGED
PR #47: Performance optimization    — 🔄 MERGING...
PR #48: Security hardening          — ⏳ PENDING
PR #49: UX polish                   — ⏳ PENDING
```

## Step 4/5: Final Report

```bash
echo "🔀 merge-chain [4/5] generating final report"
```

```bash
echo "🔗 [merge-chain:4] generating final report"
```

Display the complete results:

```
🔗 Merge Chain Complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PR #46: Dead code cleanup           — ✅ MERGED
PR #47: Performance optimization    — ✅ MERGED
PR #48: Security hardening          — ✅ MERGED (conflict resolved)
PR #49: UX polish                   — ✅ MERGED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
All 4 PRs merged successfully.
```

If the chain was stopped due to unresolved conflicts:
```
🔗 Merge Chain — Stopped at PR #48
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PR #46: Dead code cleanup           — ✅ MERGED
PR #47: Performance optimization    — ✅ MERGED
PR #48: Security hardening          — ❌ CONFLICT (app/Models/User.php, routes/web.php)
PR #49: UX polish                   — ⏳ PENDING (blocked)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Resolve conflicts on branch 'feat/security-hardening' and re-run:
  /merge-chain 48 49
```

## Flags Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--base <branch>` | auto-detect | Override the base branch for all merges |
| `--force-resolve` | off | Auto-resolve ALL conflicts preferring base branch (dangerous) |
