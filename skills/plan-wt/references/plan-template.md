# Plan Document Template v2

Use this exact structure when creating plan files in `.planning/`.

## Filename Convention

`.planning/NNNN-<type>-<short-description>-todo.md`

Types: feat, fix, docs, style, refactor, perf, test, build, chore, hotfix

## YAML Frontmatter

### Fields set by `/plan` (creation)

```yaml
---
id: "0042"
title: "feat: Short Description"
type: feat                     # feat|fix|refactor|test|style|chore|docs|hotfix
status: todo                   # todo → awaiting-review → approved → in-progress → completed → canceled
project: my-project            # git repo name
branch: feat/short-description # feature branch name
base: homolog                  # target branch for PR (homolog or main)
tags:
  - payment
  - webhook
backlog: null                  # backlog item ID this was promoted from, or null
created: 2026-03-23T14:00      # ISO 8601 from date command
completed: null                # filled by /finish
pr: null                       # filled by /pr
session: null                  # filled on first execution — single resume ID
---
```

### Fields added by `/plan-review` (never set by `/plan`)

```yaml
strategy: parallel-teams       # execution strategy (see /plan-review)
reviews:                       # corrections/findings from review
  - "T2: rollback already exists — convert to verify-only"
  - "T5: use Cache::lock not DB lock"
```

### Removed fields (and why)

| Removed | Reason |
|---------|--------|
| `plan_file` | Self-referential — file knows its own name |
| `phases_total` / `phases_done` | Computed by agent from `### Phase` count |
| `tasks_total` / `tasks_done` | Computed by agent from `[x]` vs `[ ]` count |
| `sessions` (array) | Single `session` field for resume. History lives in git log |
| `base_branch` | Renamed to `base` — shorter, same meaning |
| `name: plan` | Unnecessary — it's a plan file by convention |

## Obsidian Property Type Schema

These are the Obsidian property types for each frontmatter field. Use these when registering properties in Obsidian settings.

| Field | Obsidian Type | Notes |
|-------|--------------|-------|
| `id` | Text | Zero-padded 4-digit string |
| `title` | Text | `type: Short Description` |
| `type` | Text | Enum: feat, fix, refactor, etc. |
| `status` | Text | Enum: todo → in-progress → completed |
| `project` | Text | Git repo name |
| `branch` | Text | Feature branch name |
| `base` | Text | Target branch for PR |
| `tags` | List | Multiline YAML list (not inline) |
| `backlog` | Text | Backlog item ID or null |
| `created` | Date | ISO 8601: `2026-03-23T14:00` |
| `completed` | Date | ISO 8601 or null |
| `pr` | Text | PR URL or null |
| `session` | Text | Resume session ID or null |
| `strategy` | Text | Added by /plan-review |
| `reviews` | List | Added by /plan-review |

**Key rule:** `tags` must use multiline list format (not inline `[a, b]`) so Obsidian recognises the field as type List and enables tag filtering in the vault.

## Body Structure

The body has exactly 5 sections. No blockquote status bar, no rules block, no session log.

```markdown
# {title}

## Goal
{1-3 sentences: what this plan achieves and why it matters}

## Non-Goals
{What is explicitly OUT of scope — prevents scope creep during execution}

## Context

{Only what the executing agent NEEDS. Key file paths, constraints, interfaces.
Bullet list, not prose. No essays.}

- `app/path/to/file.php` — what it does and why it matters
- `app/other/file.php:L40-87` — specific lines if relevant
- Constraint: must be idempotent / backward-compatible / etc.
- Queue: `queue-name` (existing|new)
- No API changes / New route: POST /api/...

## Tech Stack Versions

{Auto-filled by /plan using `sdlc detect-stack --versions` + lockfile analysis.
Workers and Context7 use this to fetch correct docs and follow framework best practices.}

- {Framework} {version}, {Language} {version}, {Key packages with versions}
- {Test runner} {version}, {UI framework} {version}
- {Deployment note: e.g., "Not deployed — migrate:fresh is safe" or "Production — migrations must be additive"}

Example (Laravel):
- Laravel 13.2.0, PHP 8.4, Livewire 4.2.2 (Volt SFC)
- Pest 4, DaisyUI + Tailwind 4
- Not deployed — existing migrations can be modified, migrate:fresh is safe

Example (Next.js):
- Next.js 15.2.0, React 19, TypeScript 5.7
- Vitest 3.1, Tailwind 4, Prisma 6.4
- Deployed on Vercel — migrations must be backward-compatible

Example (Go):
- Go 1.24, Chi router, sqlx
- Go test, testify
- Deployed — DB migrations via golang-migrate

## Phases

### Phase 1: {Descriptive Name}

**Touches:** `app/Path/To/`, `resources/views/`

- [ ] Task description — specific and implementable
- [ ] Another task — one action per checkbox
- [ ] Create test for X scenario

**Verify:** `<test-command> --filter="RelevantTest"` (see project CLAUDE.md for exact command)

### Phase 2: {Descriptive Name}

**Touches:** `app/Other/Path/`

- [ ] Task description
- [ ] Task description

**Verify:** `<test-command> --filter="OtherTest"` (see project CLAUDE.md for exact command)

### Phase N: Tests

**Touches:** `tests/Feature/`

- [ ] Test scenario A
- [ ] Test scenario B
- [ ] Regression: all existing related tests pass

**Verify:** `<test-command> --filter="FullScope"` (see project CLAUDE.md for exact command)

## References
- [[related-plan-or-doc]]       # wikilinks for Obsidian graph view
- [[another-reference]]

## Acceptance
- [ ] Criterion that proves the plan succeeded
- [ ] Another criterion
- [ ] All existing related tests pass
```

**Important:**
- Tasks are left **unmarked** (no `[H]`/`[S]`/`[O]`) — `/plan-review` adds those
- No `## Execution Strategy` section — `/plan-review` adds that
- No blockquote status bar — frontmatter is the single source of truth
- No rules block — execution rules live in `/plan-approved` skill
- Status starts as `todo`

## After `/plan-review`

`/plan-review` modifies the plan by:
1. Adding `[H]`/`[S]`/`[O]` markers to every task
2. Adding `## Execution Strategy` section after Phases
3. Updating frontmatter with `strategy` and `reviews`
4. Setting status to `approved`
5. Does NOT change Goal, Non-Goals, Context, or Acceptance (unless a review correction demands it)

## Task Completion Format

When executing, tasks are marked complete with a timestamp. This format is enforced by `/plan-approved`, not by the plan file itself.

```markdown
- [x] [H] Create HublaWebhookController ✅ 2026-03-23T14:32
- [ ] [S] Map Hubla payload → ProcessaPagamento input format
```

Rules (enforced by execution skills, NOT written in plan):
- Timestamp from `date "+%Y-%m-%dT%H:%M"` — never guessed
- `✅` emoji marks completion visually
- `/commit` after each phase
- **Never collapse phases** — keep all tasks visible

## Phase Collapsing — REMOVED

**Do NOT collapse phases.** Keep all tasks visible in the plan file at all times. The full task list with `[x]` marks is the audit trail — git history is not a substitute for readable plan state.

## Getting Plan Metadata

Use `~/.claude/bin/bravros meta` to get plan metadata as JSON.
Returns: `next_num`, `base_branch`, `plan_file`, `project`, `team`, `git_remote`.

Note: `bravros meta` returns `base_branch` (the CLI's field name). In plan frontmatter, use `base` instead.
