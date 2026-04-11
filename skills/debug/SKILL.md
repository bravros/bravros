---
name: debug
description: >
  Investigate bugs and errors by spinning up parallel subagents to find root causes — then hand off
  to /backlog or /plan based on complexity. Use this skill whenever the user says "/debug",
  "debug this", "debug the error", "debug why", or any explicit /debug invocation with a bug
  description, error message, failing test, or unexpected behavior. This skill NEVER modifies
  application code — it investigates only, producing a diagnostic report with root cause analysis
  and a recommended next step (backlog item or plan). It auto-detects the project stack
  (Laravel, React Native, Node, etc.) and leverages installed tools (Sentry MCP, Laravel Boost MCP,
  log files, test output) to triangulate the issue from multiple angles simultaneously.
---

# Debug: Parallel Bug Investigation

Investigate bugs by dispatching up to 3 parallel subagents that attack the problem from different angles — code tracing, log/error analysis, and test reproduction. Once the root cause is found, hand off to `/backlog` or `/plan` depending on complexity.

```
/debug → [investigate] → [root cause report] → /backlog add  (simple fix)
                                               → /plan        (complex fix)
                                               → /backlog add + GH issue (external/blocking)
```

## The Golden Rule

**This skill NEVER modifies application code.** No edits, no fixes, no "quick patches." Debug investigates, diagnoses, and reports. The fix happens through the proper SDLC pipeline — `/backlog add` for capture, `/plan` for implementation. This separation exists because rushing a fix without understanding the full scope is how you introduce new bugs.

The only files `/debug` may create or modify are:
- `.planning/debug/` — diagnostic report files (temporary working artifacts)
- Nothing else. Ever.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/7: Environment Detection

```bash
echo "🧪 debug [1/7] detecting project environment"
```

Auto-detect the project stack by checking for framework markers. This determines which investigation tools and patterns to use.

```bash
FRAMEWORK=$(bravros meta --field stack.framework)
```

### Stack-Specific Tools

| Stack | Investigation Tools |
|-------|-------------------|
| **Laravel** | `storage/logs/laravel.log`, `php artisan tinker`, `vendor/bin/pest --filter`, Laravel Boost MCP (if available) |
| **React Native / Expo** | Metro logs, `npx jest --filter`, device logs, error boundaries |
| **Node / Next.js** | `.next/` build output, `jest`/`vitest`, server logs |
| **Generic PHP** | Error logs, `php -l` (lint), test runner |
| **Python** | `pytest`, traceback analysis, `pip list` |

**MCP Detection:** Check which MCPs are available for enhanced investigation:
- **Sentry MCP** → Always search for related errors (auto-detect organization and project)
- **Laravel Boost MCP** → Use for route/model/config inspection if available
- **n8n MCP** → Check workflow execution logs if the bug involves automations

Don't fail if an MCP isn't connected — gracefully fall back to file-based investigation.

## Step 2/7: Triage + Investigation Plan

```bash
echo "🧪 debug [2/7] triaging bug and planning investigation"
```

Read the user's bug description from `$ARGUMENTS`. Categorize the bug and decide the investigation angles:

### Bug Categories

| Category | Signal | Primary Investigation |
|----------|--------|----------------------|
| **Test failure** | "test fails", "pest fails", specific test name | Run the failing test, read assertion output, trace to source |
| **Runtime error** | Stack trace, error message, exception class | Parse the trace, check logs, reproduce with targeted test |
| **Unexpected behavior** | "should do X but does Y", "wrong data" | Trace the data flow, check business logic, inspect DB state |
| **Performance** | "slow", "timeout", "memory" | Check query logs, profile endpoints, inspect N+1 patterns |
| **Integration** | "API fails", "webhook broken", "Sentry shows" | Check external service logs, request/response payloads, retry history |

### Choose Up to 3 Investigation Angles

Based on the category, pick the most useful combination:

1. **Code Trace Agent** — Follow the execution path from entry point to failure. Read the relevant source files, trace method calls, check for logic errors, missing null checks, wrong conditionals.

2. **Log & Error Agent** — Parse `storage/logs/laravel.log` (last 100 lines around the timeframe), check Sentry for related issues via MCP, look for patterns in error frequency and stack traces.

3. **Test & Reproduce Agent** — Run the failing test (or write a minimal reproduction test that doesn't get committed), check test output, compare expected vs actual, inspect factory/seeder data.

Not every bug needs all 3 angles. A simple test failure might only need angles 1 + 3. A mysterious production error might need all 3.

## Step 3/7: Dispatch Parallel Investigation

```bash
echo "🧪 debug [3/7] dispatching investigation agents"
```

**Dispatch ALL investigation agents in a SINGLE message** — they run simultaneously. Max 3 agents. Always set `model: "sonnet"` on these Agent calls.

Each agent prompt MUST include:

1. **Working directory** — `cd /path/to/project`
2. **Bug context** — The user's description, any error messages, stack traces
3. **Investigation scope** — What this specific agent should look at
4. **Stack info** — Framework, versions, relevant packages detected in Step 1
5. **Read-only constraint** — "You are investigating only. Do NOT modify any application code, test files, config files, or any other project files. Your output is a diagnostic report only."
6. **Output format** — Write findings to `.planning/debug/agent-N-findings.md`

### Agent Prompt Template

```
You are debug-agent-{N}, investigating a bug in a {STACK} project.

Working directory: {PROJECT_PATH}
Bug description: {USER_DESCRIPTION}

YOUR INVESTIGATION ANGLE: {ANGLE_DESCRIPTION}

HARD CONSTRAINTS:
- You are READ-ONLY. Do NOT edit, create, or delete any project files.
- The ONLY file you may create is: .planning/debug/agent-{N}-findings.md
- Do NOT run destructive commands (migrate:fresh, cache:clear in production, etc.)
- Do NOT install packages or modify dependencies.
- You may run read-only commands: pest --filter (to reproduce), artisan tinker (to query), git log/diff/blame, cat/grep/find on source files.

INVESTIGATION STEPS:
{ANGLE_SPECIFIC_STEPS}

WRITE YOUR FINDINGS to .planning/debug/agent-{N}-findings.md in this format:

# Agent {N}: {ANGLE_NAME}

## What I Checked
- [list of files read, commands run, queries executed]

## Findings
- [numbered list of observations — be specific with file paths and line numbers]

## Root Cause Assessment
- **Confidence:** [high/medium/low]
- **Root cause:** [one sentence]
- **Evidence:** [file:line, log entry, test output that supports this]

## Suggested Fix Direction
- [describe WHAT to fix, not HOW — no code suggestions, just "the validation rule in FormRequest is missing X" or "the Eloquent relationship is wrong on Model Y"]
```

### Sentry Integration

If Sentry MCP is available, one of the agents (typically the Log & Error agent) should:

```
1. Use mcp sentry whoami to confirm connection
2. Use mcp sentry search_issues with the error message or exception class
3. Use mcp sentry search_events for recent occurrences
4. Include Sentry issue URL and event count in findings
```

### Laravel Boost MCP Integration

If Laravel Boost MCP is available:

```
1. Use it to inspect routes related to the failing endpoint
2. Use it to check model relationships and attributes
3. Use it to verify config values that might affect the behavior
```

## Step 4/7: Synthesize Findings

```bash
echo "🧪 debug [4/7] synthesizing findings"
```

Once all agents return, read ALL `.planning/debug/agent-*-findings.md` files. Synthesize into a unified diagnosis:

### Convergence Check

- **Strong convergence** (2-3 agents point to same root cause) → High confidence diagnosis
- **Partial convergence** (agents found related but different issues) → Multiple contributing factors
- **Divergence** (agents found unrelated things) → Likely need deeper investigation, or the bug is a symptom of something else

### Diagnostic Report

Create `.planning/debug/diagnosis-{TIMESTAMP}.md`:

```markdown
# Bug Diagnosis: {SHORT_DESCRIPTION}

**Investigated:** YYYY-MM-DDTHH:MM
**Confidence:** high | medium | low
**Agents dispatched:** N

## Summary
One paragraph: what's broken, why, and where.

## Root Cause
- **File(s):** `path/to/file.php:NN`
- **Issue:** [specific description — wrong condition, missing validation, stale cache, race condition, etc.]
- **Evidence:** [what confirmed this — test output, log entry, Sentry event, code trace]

## Contributing Factors
- [any secondary issues discovered that aren't the root cause but are related]

## Impact Assessment
- **Severity:** critical | high | medium | low
- **Affected:** [what users/features/data are impacted]
- **Scope:** [isolated to one file/component, or cross-cutting]

## Recommended Fix Direction
- [describe what needs to change — not code, but intent]
- [estimated complexity: small/medium/large]

## Sentry
- Issue: [URL if found]
- Events: [count and timeframe]
- First seen: [date]
```

## Step 5/7: Recommend Action ⛔ MANDATORY HANDOFF

```bash
echo "🧪 debug [5/7] recommending action"
```

You MUST present options via AskUserQuestion. You CANNOT apply fixes yourself.
The CLI will physically block any file edits outside .planning/ while /debug is active.

⛔ **STOP. Use `AskUserQuestion`:**
- **"/quick — fix now" (Recommended if scope ≤ 2 files)** — Hands off root cause + fix direction to /quick
- **"/backlog — capture for later"** — Creates backlog item with debug context
- **"/plan — needs a plan"** — Creates plan with debug findings

## Debug Context Handoff Format

```
## Debug Handoff
**Root cause:** {one-line summary}
**Affected files:** {list}
**Fix direction:** {what needs to change}
**Evidence:** {key findings}
**Severity:** {critical/high/medium/low}
```

## Step 6/7: Execute Hand-Off

```bash
echo "🧪 debug [6/7] executing hand-off"
```

```bash
echo "🧪 debug [6/7] executing hand-off"
```

Based on user's choice:

### Backlog Hand-Off
Pre-fill the backlog item with diagnosis data:
- **Type:** `fix`
- **Title:** From diagnosis summary
- **What/Why/Context:** From root cause analysis, with file paths and line numbers
- **Priority:** From severity assessment
- **Size:** From complexity estimate

Invoke `/backlog add` with this context — it handles the rest.

### Plan Hand-Off
Pass the full diagnosis to `/plan`:
- The diagnosis becomes the plan's Context section
- Root cause informs the Goal
- Fix direction informs the Phases
- Impact assessment informs scope and priority

### GH Issue Hand-Off
```bash
gh issue create \
  --title "🐛 fix: {SHORT_DESCRIPTION}" \
  --body "$(cat .planning/debug/diagnosis-{TIMESTAMP}.md)" \
  --label "bug"
```

Link the issue ID back to the backlog item via a `github` field in the frontmatter.

## Step 7/7: Cleanup

```bash
echo "🧪 debug [7/7] cleanup"
```

The `.planning/debug/` directory contains working artifacts. After hand-off:
- Keep `diagnosis-*.md` — it's the archival record
- Remove `agent-*-findings.md` — intermediate working files

```bash
rm -f .planning/debug/agent-*-findings.md
```

Do NOT commit debug artifacts. They're working files that served their purpose once the diagnosis was handed off to backlog or plan.

## Rules

- **NEVER modify application code** — this is the most important rule. Investigation only.
- **NEVER run destructive commands** — no `migrate:fresh`, no `cache:clear` in production, no `git reset`
- **Max 3 parallel agents** — more adds noise without proportional signal
- **Always use `AskUserQuestion`** for hand-off decisions — never assume
- **Graceful MCP fallback** — if Sentry/Laravel Boost/etc. aren't available, investigate with standard tools (logs, tests, code reading)
- **Don't guess** — if confidence is low, say so. A "we need to investigate further" is better than a wrong diagnosis
- **Timestamp everything** — findings and diagnosis files include timestamps for traceability
- **Respect the SDLC** — debug feeds into backlog/plan, never bypasses them

Use $ARGUMENTS as the bug description, error message, or failing test name.
