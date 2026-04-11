---
name: address-recap
description: >
  Triage and action session-recap reports (skill_update_*.md files). Parses issues,
  validates each against actual skill files via parallel agents, filters out already-fixed
  and project-specific items, then presents a validated list for user approval before
  routing to /backlog or /plan. Use this skill whenever the user says "/address-recap",
  "address the recap", "process the session recap", "check the recap report", "triage recap",
  or provides a skill_update_*.md file and wants to act on it. Also triggers when the user
  says "we got another session recap" or "check this recap".
---

# Address Recap: Triage Session-Recap Reports

Process a `/session-recap` report, validate each finding against the actual codebase, and route approved fixes to `/backlog` or `/plan`.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

```
/session-recap → skill_update_*.md → /address-recap → [approval] → /backlog or /plan
```

## Why This Exists

After a long autonomous session, `/session-recap` generates a report with issues, positive findings, and skill proposals. But not every issue is actionable — some are already fixed, some are project-specific, some would break other workflows. Manually triaging each one (reading skill files, checking if the fix exists, assessing impact) takes 20-30 minutes. This skill automates that validation with parallel agents and presents only what matters.

## Step 1/6: Parse the Recap File

```bash
echo "📋 address-recap [1/6] parsing recap file"
```

Read the recap file from the argument. If no argument, check for `skill_update_*.md` files in the repo root and use the most recent one.

```bash
RECAP_FILE="${ARGUMENTS:-$(ls -t skill_update_*.md 2>/dev/null | head -1)}"
if [ -z "$RECAP_FILE" ] || [ ! -f "$RECAP_FILE" ]; then
  # Use AskUserQuestion: "No recap file found. Provide the path?"
fi
```

Parse the file and extract:

**Issues** — each `### Issue N:` section:
- Title, severity, type (bug/improvement)
- Skill affected
- What happened (the problem)
- Root cause
- Proposed fix (the suggestion)

**Positive Findings** — each `### Positive N:` section:
- What worked, which skill, what to protect

**New Skill Proposals** — each `### /skill-name` under `## New Skill Proposals`

**Priority Summary** — the table at the end (if present)

Store these as structured data for Step 2.

## Step 2/6: Classify Before Validating

```bash
echo "📋 address-recap [2/6] classifying issues"
```

Before spinning up agents, do a quick first-pass classification to skip obvious non-candidates:

| Classification | Criteria | Action |
|---------------|----------|--------|
| **Skill issue** | Skill name in `~/.claude/skills/` | Validate in Step 3 |
| **CLI issue** | Mentions `bravros`, `audit`, `rules.go` | Validate in Step 3 |
| **Project-specific** | Issue mentions project CLAUDE.md, `.env`, specific app code, framework version pinning | Skip — note as "project-specific, not a skill fix" |
| **External** | Type is "bug (external)", or caused by npm/dependency | Skip — note as "external dependency issue" |
| **New skill proposal** | Under `## New Skill Proposals` | Skip — note as "use /skill-creator for new skills" |

Only issues classified as "Skill issue" or "CLI issue" proceed to Step 3.

## Step 3/6: Validate with Parallel Agents

```bash
echo "📋 address-recap [3/6] validating issues against actual skill files"
```

For each candidate issue, launch an **Explore agent** in parallel with `model: "sonnet"`. All agents run simultaneously — dispatch them in ONE message.

Each agent receives this prompt template:

```
Research only — do NOT edit any files.

Validate this issue from a session recap:
**Issue:** {title}
**Skill:** {skill_name}
**Severity:** {severity}
**What happened:** {description}
**Proposed fix:** {proposed_fix}

Check:
1. Read the skill file: ~/.claude/skills/{skill_name}/SKILL.md
2. Is the proposed fix ALREADY implemented? Compare the fix description against the current code.
3. Would the fix BREAK any other workflow? Check for dependencies:
   - What other skills reference this skill?
   - Would changing the behavior affect /auto-pr, /auto-merge, /finish, etc.?
4. Is the fix FEASIBLE within the skill file, or does it require broader changes?

Report back:
- Verdict: ALREADY_FIXED / VALID_FIX / WOULD_BREAK / NEEDS_MORE_INFO
- Evidence: what you found in the skill file (line numbers, existing logic)
- Risk: low / medium / high
- Effort: small / medium / large
```

**Cap at 6 parallel agents** — more than that adds overhead without speed benefit. If there are more than 6 candidate issues, batch them (6 at a time, wait, then next batch).

## Step 4/6: Present Validated Summary

```bash
echo "📋 address-recap [4/6] presenting validated summary"
```

Compile agent results into a summary table. Use `AskUserQuestion` to present it.

Format:

```
Session Recap Triage: {recap_file}
Project: {project} | Session: {date_range}
Issues found: {total} | Candidates: {validated} | Skipped: {skipped}

VALIDATED ISSUES:
| # | Issue | Skill | Verdict | Risk | Effort | Action |
|---|-------|-------|---------|------|--------|--------|
| 1 | Description | /start | FIX | low | small | include |
| 3 | Description | /auto-merge | FIX | low | medium | include |
| 4 | Description | /auto-merge | FIX | low | small | include |

ALREADY FIXED:
| # | Issue | Skill | Evidence |
|---|-------|-------|----------|
| 2 | Description | /start | Step 8 already before Step 9 (line 170) |

SKIPPED:
| # | Issue | Reason |
|---|-------|--------|
| 5 | Prisma 7 breakage | External dependency — project-specific |
| 7 | Brand context | Project-specific API pattern |

POSITIVE FINDINGS (no action needed):
- Worktree isolation works well — protect this pattern
- Orchestrator pattern preserves context — keep lightweight

NEW SKILL PROPOSALS (use /skill-creator):
- /initial-setup — first-time project setup
- /fix-runtime — parse and fix build errors
```

Then ask:

```
AskUserQuestion:
"How should we handle the {N} validated fixes?"
- "Create backlog items" — one backlog item per fix
- "Create a single plan" — consolidated plan for all fixes
- "Execute now" — plan + /auto-pr immediately
- "Let me pick which ones" — select individually
```

If user picks "Let me pick which ones", present a multiSelect AskUserQuestion with each validated issue as an option.

## Step 5/6: Route to Action

```bash
echo "📋 address-recap [5/6] routing approved fixes"
```

Based on user's choice:

### Option A: Backlog
For each approved fix, run `/backlog` with the issue details:
```
/backlog {severity} — {skill}: {title}. {proposed_fix}
```

### Option B: Plan
Create a single `/plan` with all approved fixes consolidated:
```
/plan Fix {N} skill issues from {project} session recap:

## Issue {num}: {skill} — {title}
{proposed_fix}
...repeat for each approved fix...

Skills to modify:
- {list of affected skill files}
```

### Option C: Execute Now
Same as Option B, but chain to `/auto-pr` after plan creation:
```
/plan --auto {same content as Option B}
```
Then the auto-pr pipeline handles review → execute → PR.

## Step 6/6: Clean Up

```bash
echo "📋 address-recap [6/6] cleaning up and archiving recap"
```

After routing:
- Move the recap file to `.planning/recaps/` (create dir if needed):
  ```bash
  mkdir -p .planning/recaps
  git mv "$RECAP_FILE" .planning/recaps/
  git commit -m "📋 plan: archive session recap $RECAP_FILE"
  ```
- Report what was done:
  ```
  Recap processed:
    - {N} fixes → {backlog/plan/auto-pr}
    - {M} already fixed (no action)
    - {K} skipped (project-specific/external)
    - Recap archived to .planning/recaps/
  ```

## Rules

- NEVER edit skill files directly — this skill only triages and routes
- ALWAYS use parallel Explore agents for validation — never validate inline
- ALWAYS use AskUserQuestion for approval — never auto-approve fixes
- Skip "New Skill Proposals" — those use /skill-creator, not this skill
- Positive findings are informational only — display but don't action
- Cap parallel agents at 6 per batch
- If a recap file has 0 actionable issues after validation, tell the user and stop
