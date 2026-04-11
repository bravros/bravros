---
name: report
description: >
  Production Investigation & Reporting — structured investigation of production incidents,
  data issues, and support cases. Creates reports in .planning/reports/ with proper frontmatter,
  evidence collection, and timeline building. Use this skill whenever the user says "/report",
  "investigate", "what happened with", "incident report", "create a report", "production issue",
  or any request to investigate a production problem or create an investigation document.
  Also triggers on "look into", "check what happened", "debug production", or entity-specific
  queries like "investigate pedido 18391".
---

# Report: Production Investigation & Reporting

```
/report <description>           → Start new investigation
/report <number>                → View existing report
/report open                    → List open reports
/report close <number>          → Mark as resolved, rename -open → -complete
/report escalate <number>       → Create plan or backlog from report
```

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## How /report differs from /debug

| Aspect | /debug | /report |
|--------|--------|---------|
| Focus | Code bugs — find broken logic | Data investigation — query DB, build timeline |
| Input | Error message, stack trace | Entity ID, user complaint, production incident |
| Output | Handoff to /quick, /backlog, or /plan | `.planning/reports/` document with evidence |
| Database | Rarely queries DB | Primary tool — builds data timelines |
| Scope | Single bug, single fix | May span multiple systems, multiple entities |

## Critical Rules

1. **Reports live in `.planning/reports/`** — never in the repo root or other locations
2. **Always get next report ID from CLI:** `bravros nextid` → use the `report` field
3. **PT-BR for user-facing content** — investigation details in Portuguese when project is PT-BR
4. **Read-only on production** — SELECT only, never UPDATE/DELETE without explicit user approval
5. **Always use AskUserQuestion before SSH/production queries** — get initial approval

## Step 1/6: Initialize Investigation

```bash
echo "📊 report [1/6] initializing investigation"
```

Reserve report ID:
```bash
REPORT_ID=$(bravros nextid | jq -r '.report')
```

Create report file at `.planning/reports/${REPORT_ID}-<type>-<slug>-open.md` using the template from `references/report-template.md`.

## Step 2/6: Gather Evidence

```bash
echo "📊 report [2/6] gathering evidence"
```

Depending on the investigation type:
- **Database queries**: Run SELECT queries to build a timeline
- **Log analysis**: Search application logs for relevant entries
- **Code review**: Trace the code path that handled the entity
- **Git blame**: Find when relevant code was changed

Document all evidence in the report with timestamps and sources.

## Step 3/6: Build Timeline

```bash
echo "📊 report [3/6] building timeline"
```

Construct a chronological timeline of events:
```markdown
## Timeline

| Time | Event | Source |
|------|-------|--------|
| 2026-04-07 14:00 | Order created | DB: orders |
| 2026-04-07 14:05 | Payment processed | DB: transactions |
| 2026-04-07 14:10 | Webhook received | logs: laravel.log |
```

## Step 4/6: Identify Root Cause

```bash
echo "📊 report [4/6] identifying root cause"
```

Analyze the evidence to identify the root cause. Update the report frontmatter:
```yaml
root_cause: "one-line summary"
```

## Step 5/6: Recommend Action

```bash
echo "📊 report [5/6] recommending action"
```

Based on findings, recommend next steps:
- If code fix needed → suggest `/plan` or `/quick` with report context
- If data fix needed → document exact queries for user approval
- If configuration issue → document what needs to change
- If third-party issue → document evidence for vendor communication

## Step 6/6: Finalize Report

```bash
echo "📊 report [6/6] finalizing report"
```

### Mode-Aware Completion

```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  echo "STATUS: report-complete. REPORT: $REPORT_ID"
else
  # Interactive mode
fi
```

⛔ **STOP. Use `AskUserQuestion`:**
- **"Escalate to /plan — create implementation plan from findings" (if code fix needed)**
- **"Escalate to /backlog — capture for later"**
- **"Close report — no action needed"**
- **"Generate user report — create PDF summary for stakeholder"** → chains to `/user-report`

## Commands

### `/report open` — List open reports
```bash
ls -la .planning/reports/*-open.md 2>/dev/null
```

### `/report close <number>` — Close a report
```bash
# Rename -open.md to -complete.md
# Update frontmatter: status: resolved, resolved: <now>
# Run updateWikilinks via sdlc finish
git mv .planning/reports/R-NNNN-...-open.md .planning/reports/R-NNNN-...-complete.md
```

### `/report escalate <number>` — Create plan from report
Read the report, extract root cause and affected files, then invoke `/plan` with report context as input.
