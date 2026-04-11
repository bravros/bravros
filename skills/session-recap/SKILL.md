---
name: session-recap
description: >
  Auto-analyze the current session and generate skill improvement recommendations.
  Use this skill whenever the user says "/session-recap", "recap the session",
  "session review", "what can we improve", "skill improvements", "analyze this session",
  "session feedback", or any request to review the session for workflow improvements.
  Also triggers on "auto improve skills", "skill update", "what went wrong",
  "how did the skills perform", or "generate improvement report".
  This skill turns every session into a feedback loop for continuous skill optimization.
---

# Session Recap: Auto-Improve Skills from Real-World Execution

Analyze the current session's skill invocations, identify failures/slowdowns/workarounds, and generate actionable improvement recommendations.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

```
[any workflow session] → /session-recap → skill_update_{branch}.md
```

## Critical Rules

- You MUST analyze the ACTUAL session context — never fabricate issues or improvements.
- Only recommend changes backed by concrete evidence from this session.
- Separate real bugs from "could be nicer" — prioritize what actually failed or slowed down.
- Do NOT modify any skill files directly. Output is a recommendation file only.
- Include real examples from the session (sanitized if needed) to justify each recommendation.

## Audit Log Integration

At the start of session-recap, extract audit data:
```bash
SESSION_ID=$(echo "$SESSION_ID" | cut -c1-8)
LOG_FILE="$HOME/.claude/hooks/logs/$(date +%Y-%m-%d).log"
grep "SESSION:$SESSION_ID" "$LOG_FILE" > /tmp/session-audit.log 2>/dev/null

BLOCKS=$(grep -c "BLOCK:" /tmp/session-audit.log 2>/dev/null || echo 0)
WARNS=$(grep -c "WARN:" /tmp/session-audit.log 2>/dev/null || echo 0)
TOOL_CALLS=$(grep -c "CALL |" /tmp/session-audit.log 2>/dev/null || echo 0)
```

Include in recap: "Audit: N tool calls, M blocked, K warnings. Rules triggered: [list]"

## Step 1/5: Gather Session Context

Run: `echo "📋 session-recap [1/5] gathering session context"`

Collect metadata about the session:

```bash
# Get current branch and project info
BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
PROJECT=$(basename "$(pwd)")

# Get plan info if available
~/.claude/bin/bravros meta 2>/dev/null || echo "No plan context"

# Get recent commits from this session (last 2 hours)
git log --oneline --since="2 hours ago" 2>/dev/null | head -20
```

## Step 2/5: Analyze Skill Invocations

Run: `echo "📋 session-recap [2/5] analyzing skill invocations"`

Review the conversation context and identify every skill that was invoked. For each skill, evaluate:

### 2a: Execution Quality

| Dimension | Question |
|-----------|----------|
| **Correctness** | Did the skill produce the right result on first try? |
| **Speed** | Were there unnecessary sequential steps that could be parallel? |
| **Worker reliability** | Did spawned agents follow instructions completely? |
| **Manual intervention** | Did the user or coordinator have to fix something the skill should have handled? |
| **Error handling** | Were errors caught and handled, or did they cascade? |

### 2b: Identify Patterns

Look for these common failure modes:

| Pattern | Description | Example |
|---------|-------------|---------|
| **Worker drift** | Spawned agents skip parts of their instructions | Worker doesn't mark tasks, doesn't use /ship |
| **Stale detection** | Skill re-processes old data instead of detecting "nothing new" | address-pr re-analyzing same review |
| **Missing gate** | Skill proceeds when it should stop and ask | Merging without checking CI |
| **Redundant work** | Same information gathered multiple times | Reading same file in coordinator + worker |
| **Context waste** | Coordinator does work that should be delegated | Leader debugging instead of dispatching |
| **Ordering issue** | Steps happen in wrong order causing rework | Committing before marking tasks |

### 2c: Identify Positive Patterns

Also capture what worked well — skills that executed cleanly should be noted so we don't accidentally regress them:

- Skills that ran without any manual intervention
- Parallel dispatches that saved time
- Error recovery that worked correctly
- Defensive checks that caught real issues

### 2d: Identify SDLC Tooling Issues

Separately from project-specific issues, look for problems with the SDLC tooling itself:

| Signal | Description | Example |
|--------|-------------|---------|
| **CLI failures** | A `bravros` subcommand failed or required a workaround | `bravros finish` picked the wrong plan, `merge-pr` failed and needed manual merge |
| **Skill wrong output** | A skill produced incorrect output that needed manual correction | Worker used wrong model tier, quality sweep missed an obvious issue |
| **Model tier mismatch** | Opus used where Sonnet/Haiku would suffice, or Haiku used where Sonnet was needed | Coordinator spawned Haiku for architecture decision, Opus used for a simple rename |
| **Missing quality rule** | A PR review caught something the worker or quality sweep should have caught automatically | IDOR not in worker prompts, schema mismatch not checked, migration ordering not validated |
| **Performance win** | A pattern was faster than expected — preserve it | Parallel dispatch to 4 workers cut time in half vs sequential; batch commit saved N round trips |

These findings belong in the **SDLC Improvements** section of the report (separate from project-level issues).

## Step 3/5: Classify Findings

Run: `echo "📋 session-recap [3/5] classifying findings"`

For each finding, classify:

| Field | Values |
|-------|--------|
| **Severity** | `high` (blocked progress / required manual fix), `medium` (slowed down / suboptimal), `low` (cosmetic / minor), `info` (observation only) |
| **Type** | `bug` (skill didn't work as designed), `improvement` (works but could be better), `new-skill` (gap that needs a new skill), `positive` (worked great — protect this) |
| **Effort** | `small` (< 10 lines changed), `medium` (new section or logic), `large` (significant rewrite or new skill) |

## Step 4/5: Generate Report

Run: `echo "📋 session-recap [4/5] generating improvement report"`

Determine the output filename:

```bash
BRANCH=$(git branch --show-current 2>/dev/null || echo "manual")
# Sanitize branch name for filename
SAFE_BRANCH=$(echo "$BRANCH" | tr '/' '-')
# $PORTABLE_REPO defaults to ~/Sites/claude (macOS) or ~/claude (Linux)
PORTABLE_REPO="${PORTABLE_REPO:-$HOME/Sites/claude}"
OUTPUT="${PORTABLE_REPO}/skill_update_${SAFE_BRANCH}.md"
```

Write the report using this structure:

```markdown
# Skill Improvement Report — Session YYYY-MM-DD

**Branch:** {branch}
**Project:** {project}
**Flow:** skill1 -> skill2 -> skill3 -> ...
**Duration:** ~Xm (estimated from commit timestamps)
**Skills invoked:** N total, N clean, N with issues

---

## Findings

### Issue N: {Title}
**Severity:** high/medium/low
**Type:** bug/improvement/new-skill
**Skill:** {skill-name}
**What happened:** {concrete description with timestamps or commit refs}
**Root cause:** {why it happened}
**Proposed fix:**
```
{specific code/text changes to the skill file}
```
**Evidence:** {paste from session — the actual output or behavior observed}

---

### Positive N: {Title}
**Skill:** {skill-name}
**What worked:** {description}
**Protect:** {what to NOT change}

---

## SDLC Improvements

> Issues with the tooling itself — CLI, skills, model tiers, quality rules, performance patterns.
> These are separate from project-specific bugs and should be actioned in ~/Sites/claude, not in the project.

### SDLC Issue N: {Title}
**Category:** cli-failure / skill-wrong-output / model-tier-mismatch / missing-quality-rule / performance-win
**Severity:** high/medium/low/info
**Tool/Skill:** {bravros subcommand or skill name}
**What happened:** {concrete description}
**Root cause:** {why it happened}
**Proposed fix:** {specific change to CLI, skill file, worker prompt, or quality rule}
**Evidence:** {paste from session}

---

## New Skill Proposals

### /skill-name
**Purpose:** {what it does}
**Trigger:** {when to invoke}
**Why:** {what gap it fills, with session evidence}

---

## Priority Summary

| # | Issue | Skill | Severity | Type | Effort |
|---|-------|-------|----------|------|--------|
| 1 | ... | ... | high | bug | small |

---

## Auto-Apply Candidates

Items marked as `small` effort + `high` severity are candidates for immediate
application. List them here with exact file paths and diffs.
```

## Step 5/5: Present and Offer Next Steps

Run: `echo "📋 session-recap [5/5] report complete"`

Display a summary of findings, then use `AskUserQuestion`:

- **Question:** "Session recap generated with N findings. What next?"
- **Option 1:** "Apply high-priority fixes now" — Create a branch and apply small+high items directly to skill files
- **Option 2:** "Add to backlog" — Create backlog items for each finding
- **Option 3:** "Just the report" — File saved, I'll review manually

## Notes

- The report file is written to `$PORTABLE_REPO` (the portable skill repo — `~/Sites/claude` on macOS, `~/claude` on Linux), NOT to the project directory.
- If `skill_update_{branch}.md` already exists (e.g., from a previous recap in the same session), append a timestamp suffix.
- This skill does NOT modify any skill files. It only produces recommendations. The user decides what to apply.
- For `/auto-merge` sessions with multiple plans, the recap covers ALL plans in the batch.
- Old `skill_update_*.md` files can be cleaned up periodically — they are reference artifacts, not permanent config.
