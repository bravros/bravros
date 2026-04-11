---
name: status
description: >
  Show quick repo status — branch, base, plan, PR info in one shot.
  Use this skill whenever the user says "/status", "show status", "repo status",
  "what branch am I on", or any request to see the current project state.
  Also triggers on "current status", "where am I", "project status", "git status",
  "what's the current state", "which branch", "is there a PR open", or "what plan am I on".
---

# Status: Quick Repo Context

Show quick repo status (branch/base/plan/PR) in one shot.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Data Sources
This skill uses **git** and **gh** commands (via `bravros full`) to gather real-time repo state. Data must reflect the current git state — never cache or reuse stale data from prior calls.

## Run

Try `bravros full` first. If it fails, fall back to direct commands.

### Primary
```bash
~/.claude/bin/bravros full
```

### Fallback (if bravros is unavailable or errors)
Run these git/gh commands directly:
```bash
git branch --show-current
git log --oneline -3
git status --short
gh pr view --json number,title,url,state 2>/dev/null || echo "No open PR"
```
And check `.planning/` for any active plan file.

### Batch Mode Detection

Check for active batch pipeline:
```bash
if [ -f ".planning/.batch-progress.json" ]; then
  cat .planning/.batch-progress.json
fi
```

If the file exists, display the batch pipeline status using the emoji table format:

```
📋 Batch Pipeline — Plans NNNN to MMMM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Plan 0034: Dead code cleanup           — ✅ MERGED
Plan 0035: Performance optimization    — ✅ MERGED
Plan 0036: Security hardening          — 🔄 EXECUTING
Plan 0037: UX & responsive polish      — ⏳ QUEUED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Progress: 2/4 complete
```

Status emoji mapping:
- `merged` → ✅ MERGED
- `executing` → 🔄 EXECUTING
- `queued` → ⏳ QUEUED
- `failed` → ❌ FAILED
- `skipped` → ⏭️ SKIPPED

## Rules
- Do NOT modify anything — this is a read-only operation.
- Do NOT scan or analyze code — status is metadata only.
- Data must be fresh — always run commands, never rely on cached or prior results.

## Output
Present results in a scannable format — not a wall of text. Show:
- Current branch + base branch
- Active plan (if any)
- Open PR (if any)
- Uncommitted changes summary

When a batch pipeline is active (`.planning/.batch-progress.json` exists), the batch status table is shown above the normal repo status.

All info in one response — no follow-up needed.