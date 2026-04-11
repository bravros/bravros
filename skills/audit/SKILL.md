---
name: audit
description: >
  Full project audit pipeline — spawn parallel audit subagents, collect findings, create backlog items,
  and optionally promote to plans. Single command replaces manual 3-step orchestration.
  Use this skill whenever the user says "/audit", "audit the project", "check everything",
  "find all issues", "full project scan", "health check", "what needs fixing",
  "run all audits", "audit security", "audit performance", or any request to scan
  the project for issues across multiple areas. Also triggers on "project health",
  "code quality check", "find problems", "scan for issues".
---

# Audit: Full Project Audit Pipeline

Run a comprehensive project audit by dispatching parallel subagents across multiple areas, collecting findings, creating backlog items, and optionally promoting them to plans. One command replaces the manual 3-step orchestration (spawn agents -> create backlogs -> create plans).

```
/audit                              → Audit all 6 areas (default)
/audit --areas security,performance → Audit specific areas only
/audit --promote                    → Audit + auto-promote backlogs to plans
/audit --interactive                → Ask user to classify each backlog item
```

The `$ARGUMENTS` variable contains everything after `/audit`. Parse it for flags.

---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/6: Initialize & Parse Args

```bash
echo "🧪 audit [1/6] initializing audit pipeline"
```

### Parse $ARGUMENTS

Extract these flags from `$ARGUMENTS`:

| Flag | Default | Purpose |
|------|---------|---------|
| `--areas <csv>` | `security,performance,deadcode,coverage,ux,responsive` | Comma-separated list of areas to audit |
| `--promote` | off | Auto-promote backlogs to plans after creation |
| `--interactive` | off | Use AskUserQuestion for backlog classification |

If `$ARGUMENTS` contains no flags, audit ALL areas autonomously.

If `$ARGUMENTS` contains area names without `--areas` (e.g., `/audit security performance`), treat them as the areas list.

### Auto-Detect Project Type

**Read `~/.claude/skills/context/references/stack-detection.md` (sections 1a–1b) for framework detection logic.**

Determine the project stack to tailor audit checks:

```bash
FRAMEWORK=$(bravros meta --field stack.framework)
```

Store the detected stack (e.g., `laravel`, `react-native`, `next`, `node`, `php`, `python`, `go`, `rust`) for use in agent prompts.

### Ensure Planning Directory

```bash
mkdir -p .planning/backlog/archive
```

### Determine Next Backlog ID

```bash
bravros backlog --archive --format table
```

Note the highest existing ID. The first new backlog will use the next sequential number.

---

## Step 2/6: Spawn Parallel Audit Subagents

```bash
echo "🧪 audit [2/6] spawning parallel audit subagents"
```

For each selected area, spawn an Agent with `subagent_type: Explore` and `model: "sonnet"`. **ALL agents MUST be dispatched in a single message** — do not wait for one before launching the next. Maximum 6 agents (one per area).

Each agent receives:
1. The detected project stack
2. Area-specific instructions (below)
3. A strict output format requirement

### Agent Prompt Template

Each agent gets this structure (customize the checklist per area):

```
You are an audit agent scanning this project for {AREA} issues.
Project stack: {DETECTED_STACK}

SCAN THE CODEBASE for the following issues. For EVERY finding, report:
- severity: critical | high | medium | low
- file: exact file path
- line: line number or range (if identifiable)
- description: what the issue is and why it matters

Return your findings as a numbered list in this exact format:
1. [SEVERITY] `file/path.ext:LINE` — Description of the issue

If you find zero issues in this area, return: "No findings."

DO NOT modify any files. READ ONLY.
```

### Area-Specific Checklists

**security** — Explore agent checks for:
- Hardcoded credentials, API keys, tokens, passwords in source code (all stacks)
- `.env` or secrets committed to git (all stacks)
- Exposed debug info: `dd()`, `dump()` in PHP; `console.log()` with sensitive data in JS; `print()` in Python; debug mode enabled
- Insecure file uploads: missing validation, no size limits (all stacks)

**Laravel-specific security checks:**
- SQL injection: raw queries, `DB::raw()` without bindings, string concatenation in queries
- XSS: unescaped output, `{!! !!}` in Blade without sanitization
- CSRF: forms without `@csrf`, API routes without token validation
- `env()` usage outside of config files (breaks config caching)
- Mass assignment: models without `$fillable` or `$guarded`
- Auth bypass: routes missing middleware, policy gaps, gate checks

**Node.js / JavaScript security checks:**
- XSS: `dangerouslySetInnerHTML`, unescaped template literals, missing input sanitization
- SQL injection in raw queries (Knex, raw SQL without parameterization)
- Missing security headers (helmet/cors configuration)
- Eval usage: `eval()`, `Function()` constructor with untrusted input

**Python security checks:**
- SQL injection: string formatting in raw queries, missing parameterization
- Pickle deserialization of untrusted data
- Debug mode enabled in production (Flask/Django)
- Missing CORS/CSRF protection

**Go security checks:**
- SQL injection: string concatenation in queries without parameterization
- Missing error handling: unchecked error returns
- Race conditions: concurrent map/slice access without synchronization
- Missing input validation before SQL/system calls

**performance** — Explore agent checks for:
- N+1 queries: loops with relationship access without eager loading
- Missing database indexes on columns used in `where`, `orderBy`, `join`
- Missing eager loading (`with()`) on frequently accessed relationships
- No pagination on large datasets (`->get()` without `->paginate()`)
- Heavy queries in loops or repeated query patterns
- Missing cache usage for expensive operations
- Synchronous operations that could be queued (email, notifications, API calls)
- Large file reads without streaming
- Missing database query optimization (subqueries, raw counts vs loading collections)

**deadcode** — Explore agent checks for:
- Unused routes (defined in routes/ but controller method missing or never called)
- Unused controllers, models, services, traits, middleware, jobs, events, listeners
- Unused views/templates (Blade files not referenced by any controller or component)
- Unused imports/use statements
- Commented-out code blocks (more than 5 lines)
- Unused config keys
- Orphaned migration files (tables that no longer exist in models)
- Unused npm/composer dependencies

**coverage** — Explore agent checks for:
- Controllers without corresponding test files
- Models without corresponding test files
- Services/Actions without corresponding test files
- Livewire components without test files
- API endpoints without feature tests
- Missing edge case tests (error paths, validation failures, authorization)
- Test files that exist but have very few test cases relative to the source complexity
- Critical business logic without unit tests

**ux** — Explore agent checks for:
- Missing form validation error messages (server validates but no user feedback)
- Missing loading states on forms and buttons
- Missing empty states (lists/tables with no data)
- Missing confirmation dialogs on destructive actions (delete, archive)
- Inconsistent navigation or broken links
- Missing success/error flash messages after actions
- Accessibility: missing alt text, missing labels, missing ARIA attributes
- Missing 404/500 error pages or generic error handling

**responsive** — Explore agent checks for:
- Missing viewport meta tag
- Fixed widths that break on mobile (hardcoded px widths on containers)
- Missing responsive breakpoints on key layouts
- Overflow issues: horizontal scroll on mobile
- Touch target sizes below 44x44px
- Missing responsive images (no srcset, no responsive classes)
- Tables without responsive wrapper or mobile-friendly alternative
- Missing mobile navigation (hamburger menu, drawer)
- Media queries that don't cover standard breakpoints (sm, md, lg, xl)

---

## Step 3/6: Collect & Deduplicate Findings

```bash
echo "🧪 audit [3/6] collecting and deduplicating findings"
```

After ALL agents complete (or timeout after 5 minutes), process their results:

### 3a: Parse Agent Output

For each agent's response, extract findings into a structured list:
```
{area, severity, file, line, description}
```

If an agent failed or timed out, record it as a skipped area with reason.

### 3b: Deduplicate

Same file + same issue description (fuzzy match) = keep the one with the highest severity. This handles cases where security and performance agents both flag the same raw query, for example.

### 3c: Severity Mapping

| Severity | Priority for Backlog |
|----------|---------------------|
| critical | critical |
| high | high |
| medium | medium |
| low | low |

The backlog item inherits the priority of its **highest-severity** finding.

### 3d: Size Mapping

| Finding Count | Backlog Size |
|---------------|-------------|
| 1-3 | small |
| 4-10 | medium |
| 11+ | large |

### 3e: Output Intermediate Summary

Print this summary to the user before creating backlogs:

```
🔍 Audit Findings
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Security:     N findings (breakdown by severity)
Performance:  N findings (breakdown by severity)
Dead Code:    N findings (breakdown by severity)
Coverage:     N findings (breakdown by severity)
UX:           N findings (breakdown by severity)
Responsive:   N findings (breakdown by severity)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total:        N findings
```

---

## Step 4/6: Create Backlog Items

```bash
echo "🧪 audit [4/6] creating backlog items"
```

Create ONE backlog item per area that has findings. Skip areas with zero findings.

**IMPORTANT: Use the backlog format from `/backlog` skill exactly.**

### Filename

`.planning/backlog/NNNN-<type>-<area>-audit.md`

Where:
- `NNNN` = next sequential backlog ID (zero-padded 4 digits)
- `<type>` = determined by area mapping below
- `<area>` = the audit area name

| Area | Type |
|------|------|
| security | fix |
| performance | perf |
| deadcode | refactor |
| coverage | test |
| ux | fix |
| responsive | fix |

### Backlog File Content

```markdown
---
id: "NNNN"
title: "<type>: <area> audit findings"
type: <type>
status: new
priority: <highest-severity-finding>
size: <from-finding-count>
project: <project-name>
tags: [audit, <area>]
created: "YYYY-MM-DDTHH:MM"
plan: null
depends: null
---

# <type>: <Area> Audit Findings

## What
<Area> audit identified N issues that need attention. <1-sentence summary of the most critical finding>.

## Why
<Brief impact statement — why these findings matter for the project>.

## Context
Findings from automated audit, sorted by severity:

### Critical
- `file/path.ext:LINE` — Description

### High
- `file/path.ext:LINE` — Description

### Medium
- `file/path.ext:LINE` — Description

### Low
- `file/path.ext:LINE` — Description

## Notes
- Generated by `/audit` on YYYY-MM-DD
- Review findings before promoting to plan — some may be false positives
```

**Do NOT commit yet** — all backlog files are created as uncommitted working-directory files (consistent with `/backlog add` behavior). The commit happens in Step 6.

If `--interactive` flag is set, use AskUserQuestion after presenting the summary to let the user adjust priority/size/type for each backlog before writing the files.

---

## Step 5/6: Optional Promote to Plans

```bash
echo "🧪 audit [5/6] promoting plans"
```

**This step ONLY runs if `--promote` flag was passed.** Otherwise, skip entirely.

If `--promote` is set:

1. For each backlog created in Step 4, generate a plan file in `.planning/`
2. Use `bravros meta` to get the next plan number and base branch
3. Plan content is derived from the backlog findings:
   - **Phase 1**: Critical + High severity findings (must fix)
   - **Phase 2**: Medium severity findings (should fix)
   - **Phase 3**: Low severity findings (nice to fix)
   - Skip empty phases
4. Each plan task references the specific file and finding from the audit
5. Update the backlog frontmatter: `status: planned`, `plan: "PLAN_ID"`
6. Move backlog files to `.planning/backlog/archive/` via `git mv`

Plan files follow the standard plan template from `/plan` skill.

**Do NOT execute plans** — they are created for later execution via `/plan-approved` or `/auto-merge`.

---

## Step 6/6: Commit & Report

```bash
echo "🧪 audit [6/6] committing and reporting"
```

### Commit

Stage and commit all created files:

```bash
git add .planning/backlog/*.md
# If --promote was used, also add plan files and archived backlogs:
# git add .planning/*.md .planning/backlog/archive/*.md
```

Commit message format:
```
🧹 chore: audit project — N findings across M areas
```

Where N = total findings count and M = number of areas with findings.

### Final Report

```
🔍 Project Audit Complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Security:     ✅ N findings → Backlog #NNNN
Performance:  ✅ N findings → Backlog #NNNN
Dead Code:    ✅ N findings → Backlog #NNNN
Coverage:     ✅ N findings → Backlog #NNNN
UX:           ✅ N findings → Backlog #NNNN
Responsive:   ⏭️ 0 findings — skipped
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: N findings → M backlogs created

Next: /backlog promote N-M to create plans, or /auto-merge to execute
```

Use ✅ for areas with findings (backlog created), ⏭️ for areas with zero findings (skipped), and ❌ for areas where the agent failed/timed out.

If `--promote` was used, update the "Next" line:
```
Next: /plan-approved NNNN or /auto-merge NNNN-MMMM to execute
```

---

## Critical Rules

1. **NEVER use AskUserQuestion unless `--interactive` flag is set.** The default mode is fully autonomous — no user prompts, no confirmations, no classification questions. The audit runs, creates backlogs, and reports. Period.

2. **Maximum 6 parallel audit agents (one per area).** All agents are dispatched in a single message. Never spawn more than 6 agents total. Never spawn agents sequentially — parallelism is the entire point.

3. **Results go to `.planning/backlog/` — NEVER implement fixes directly.** The audit skill identifies problems. It does not fix them. Fixes happen through the normal SDLC pipeline: backlog -> plan -> plan-approved. This separation exists because blindly fixing audit findings without planning causes regressions.

4. **Each area agent is an Explore subagent — they READ code, they don't modify it.** Agents must use `subagent_type: Explore` (or equivalent read-only mode). If any agent attempts to write files, it is a bug.

5. **If an area agent fails or times out, skip it and note in report.** Do not retry. Do not block the pipeline. Log it as ❌ in the final report and continue with the areas that succeeded.

6. **Backlog format must match the schema in `/backlog` skill.** YAML frontmatter fields (id, title, type, status, priority, size, project, tags, created, plan, depends) must all be present. The audit hook validates this format.

7. **Findings must include file paths.** Vague findings like "improve performance" or "add tests" are not actionable and must be discarded. Every finding needs: severity + file path + description. If an agent returns vague findings, filter them out before creating the backlog.

8. **No AI signatures in commits.** Never add `Co-Authored-By`, `Generated by AI`, or any AI attribution to commit messages.

9. **Use the project's existing backlog numbering.** Always check existing backlogs first (Step 1) to determine the next sequential ID. Never hardcode IDs or start from 0001.

10. **Date format is ISO 8601 (YYYY-MM-DDTHH:MM).** Consistent with the backlog skill's `created` field format.
