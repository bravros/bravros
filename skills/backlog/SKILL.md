---
name: backlog
description: >
  Capture, manage, and promote pre-planning ideas before they become full plans.
  Use this skill whenever the user says "/backlog", "add to backlog", "backlog idea",
  "capture this idea", "save this for later", "I had an idea", or any request to manage
  the idea backlog. Also triggers on "list ideas", "promote idea", "backlog list",
  "drop idea", "mark done", "what's in the backlog", "show me the ideas", "pending ideas",
  or any request to add/view/promote/done/drop items in .planning/backlog/.
  The backlog is the pre-planning stage — ideas live here until promoted to /plan.
  Even if the user doesn't explicitly say "backlog", trigger when they describe wanting
  to capture a feature idea, task, or improvement for later without implementing it now.
  This skill is the gateway to the SDLC — nothing gets planned or built without first
  passing through the backlog.
---

# Backlog: Pre-Planning Idea Manager

The backlog is the first stage of the development lifecycle. It exists because good ideas deserve to be captured immediately — but implemented thoughtfully. When someone says "we should add notifications" during a bug fix, the backlog catches that idea so it doesn't get lost, without derailing the current work.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

Think of it as a parking lot for ideas: lightweight enough to add in seconds, structured enough to evaluate later.

## Git Policy

**Read-only commands (add, list, view, archive):** These MUST NOT run any git commands — no `git add`, no `git commit`, no `git status`, no `git mv`. They only create or read `.planning/backlog/` files. The backlog is a scratch pad; ideas are uncommitted working-directory files until they graduate.

**State-changing commands (promote, done, drop):** These DO use git because they represent decisions that should be recorded in history. Always delegate to `bravros backlog promote/done/drop` — the CLI handles frontmatter updates, `git mv`, and atomic commits internally.

This separation is intentional: capturing an idea should be instant and zero-friction. Git operations only happen when the idea's lifecycle changes.

## Commands

```
/backlog add <description>     → Capture a new idea (NO git)
/backlog                       → List all active ideas (NO git)
/backlog <number>              → View details of a specific idea (NO git)
/backlog promote <number>      → Graduate idea into a full /plan (uses git)
/backlog promote N-M          → Graduate multiple ideas into plans at once (uses git)
/backlog done <number>         → Mark as completed and archive (uses git)
/backlog drop <number>         → Discard idea and archive with reason (uses git)
/backlog archive               → List archived ideas (NO git)
```

The `$ARGUMENTS` variable contains everything after `/backlog`. Parse it to determine which command the user wants: no args = list, a number = view, "add ..." = add, "promote N" = promote, "done N" = done, "drop N" = drop, "archive" = archive.

---

## Adding an Idea (`/backlog add`)

This is the most common operation. The goal is to capture just enough context that someone (including future-you) can evaluate the idea later without needing the original conversation for context.

**IMPORTANT: `/backlog add` MUST NOT run any git commands. It writes a file to the working directory and nothing else. The file stays uncommitted — it will be committed later when promoted, or during the next `/commit`.**

### Step 1: Scan existing ideas and determine next ID

**MANDATORY: Use the CLI to list existing ideas and check for duplicates.**

```bash
mkdir -p .planning/backlog/archive
# List all existing ideas via CLI (handles both YAML and legacy formats)
bravros backlog --archive --format table
```

Check the output for potential duplicates with the user's idea.

Then get the next atomic backlog ID:
```bash
~/.claude/bin/bravros nextid
```
This returns `{"plan": "NNNN", "backlog": "NNNN"}`. Use the `backlog` value for the new idea's ID. The command creates an atomic placeholder to prevent duplicate IDs when parallel agents create backlogs.

If a potential duplicate exists, surface it before proceeding. If no duplicates, continue.

**NEVER use grep/sed/awk/cat to parse backlog files. The CLI handles all formats correctly.**

### Step 2: Classify with the user

Use `AskUserQuestion` to quickly classify the idea. Ask a single multi-part question covering:

- **Type**: `feat` (new feature), `fix` (bug), `refactor`, `perf`, `chore`, `docs`, `test`
- **Size**: `small` (< half day), `medium` (1-3 days), `large` (> 3 days)
- **Priority**: `high` (blocking or urgent), `medium` (important but not urgent), `low` (nice to have)

If the user's original message already makes these obvious (e.g., "urgent bug: login is broken"), infer what you can and only ask about what's ambiguous.

### Step 3: Write the idea file

Filename: `.planning/backlog/NNNN-<type>-<short-slug>.md`

The slug should be 2-4 words, kebab-case, descriptive enough to identify the idea from a file listing.

```markdown
---
id: "NNNN"
title: "<type>: Short Description"
type: feat
status: new
priority: high
size: medium
project: <project-name>
tags:
  - relevant
  - tags
created: 2026-03-23T14:00
plan: null
depends: null
---

# <type>: Short Description

## What
One paragraph describing what needs to happen.

## Why
Why this matters — the user impact or technical motivation.

## Context
Any relevant context: related features, technical constraints, conversations that sparked the idea.
Bullet list format — key file paths, API docs, root cause analysis, affected data.

- `app/path/to/file.php` — what it does
- Affected: N orders / N users
- Pattern to follow: `app/path/to/existing/pattern.php`

## Notes
Optional: links, references, edge cases to consider.

## References
- [[depends-on-backlog-id]]     # wikilinks for Obsidian graph view
- [[related-plan-or-doc]]
```

### Field Reference

| Field | Type | Purpose |
|-------|------|---------|
| `id` | string | Sequential, zero-padded 4 digits |
| `title` | string | `type: short description` |
| `type` | enum | feat, fix, refactor, test, chore, docs |
| `status` | enum | `new` → `ready` → `planned` → `archived` (or `on-hold`) |
| `priority` | enum | `critical`, `high`, `medium`, `low` |
| `size` | enum | `small` (≤5 tasks), `medium` (6-15), `large` (16+, may need multiple plans) |
| `project` | string | Project slug |
| `tags` | array | Topic tags — same vocabulary as plan tags |
| `created` | string | ISO 8601: `2026-03-23T14:00` |
| `plan` | string\|null | Plan ID when promoted (e.g., `"0177"`) |
| `depends` | array\|null | Other backlog IDs this depends on |

Keep it lightweight. The backlog is for capturing intent, not writing specs. A few sentences per section is ideal. If the user gave a one-liner ("add notifications"), expand it just enough to be useful later, but don't over-document.

### Step 4: Confirm (NO GIT — just confirm)

Show the user a brief summary: the idea number, title, classification, and a reminder they can promote it later with `/backlog promote NNNN`.

**Do NOT run `git add` or `git commit`.** The file is an uncommitted working-directory file. It will be picked up by the next `/commit` or when the idea is promoted/done/dropped.

---

## Listing Active Ideas (`/backlog`)

**MANDATORY: Use the CLI — never parse backlog files manually.**

### Step 1: Get data from CLI

```bash
~/.claude/bin/bravros backlog --format table
```

If the backlog is empty, say so and remind the user how to add ideas (`/backlog add <idea>`).

### Step 2: Present in priority-sorted format

After getting the CLI output, reformat into a clean table sorted by priority (critical > high > medium > low) with emoji indicators:

```
┌──────┬──────────┬──────────────────────────────────────────────┬────────────┐
│  #   │ Priority │ Title                                        │ Status     │
├──────┼──────────┼──────────────────────────────────────────────┼────────────┤
│ 0055 │ 🔴 crit  │ feat: Platform Signature Validation           │ new        │
│ 0062 │ 🟠 high  │ feat: Produtor Sidebar Afiliados Submenu      │ new        │
│ 0032 │ 🟡 med   │ feat: Push Notifications (FCM)                │ new        │
│ 0048 │ 🟢 low   │ chore: Clean up old migrations                │ new        │
└──────┴──────────┴──────────────────────────────────────────────┴────────────┘
```

Priority emoji mapping: 🔴 critical, 🟠 high, 🟡 medium, 🟢 low, ⚪ unset

### Step 3: Verify and clean stale items

If any listed item has `status: archived` or `status: planned`, archive it before presenting:
```bash
~/.claude/bin/bravros backlog done <ID>
```

Only show items that are active (`status: new`). The backlog should be a clean view of remaining work — not a historical record. Completed items belong in `.planning/backlog/archive/`.

For JSON output (useful for programmatic checks):
```bash
~/.claude/bin/bravros backlog
```

**No manual git commands. No direct file modifications. CLI only.**

---

## Listing Archived Ideas (`/backlog archive`)

**MANDATORY: Use the CLI.**

```bash
bravros backlog --archive --format table
```

This shows both active AND archived items. The archive section includes status (Done / Dropped / Planned) and plan links.

**No git commands. No file modifications. Read-only.**

---

## Viewing Details (`/backlog <number>`)

Search both `.planning/backlog/` and `.planning/backlog/archive/` for a file matching the number prefix (e.g., `0003-*`). Read and display the full contents. If not found, say so and suggest listing active ideas.

**No git commands. No file modifications. Read-only.**

---

## Promoting to Plan (`/backlog promote <number>`)

Promotion is the bridge between "idea" and "implementation." It means the idea has been evaluated and is worth investing planning time into.

1. Ask the user: "Worktree or local?" — this determines whether to run `/plan` (works on current branch) or `/plan-wt` (creates an isolated git worktree)
2. Read the idea file to extract context for the plan
3. Run the promote command — this updates status, archives the file, and commits atomically:
   ```bash
   bravros backlog promote <ID>
   ```
   Returns JSON: `{"id", "action": "promote", "archived_path", "commit"}`
4. Hand off to the chosen plan skill with the idea's context
5. Once the plan is created, the `/plan` skill sets `backlog: "NNNN"` in the plan frontmatter (bidirectional link)

> ⛔ **GUARDRAIL — NEVER skip the plan pipeline.** After promote, you MUST invoke `/plan` (or `/plan-wt`) before any implementation work begins. Even if the user says "just do it", "spin up agents", or "start coding now" — the plan file must exist and pass `/plan-review` → `/plan-approved` before any code changes occur. The only exception is `/quick` for trivial fixes.

The CLI handles: `status: planned` in frontmatter → `git mv` to archive/ → commit `📋 plan: promote backlog NNNN`.

---

## Batch Promoting (`/backlog promote N-M`)

Use this when you want to graduate multiple backlog ideas into plans in one operation. Parse the range from `$ARGUMENTS` (e.g., `promote 25-30` → promote items 0025 through 0030 inclusive).

**Important:** Batch promote does NOT create plan files itself. It prepares and archives the backlog items, then hands off to `/plan` for each one — keeping plan creation logic in a single place.

### Process

1. **Parse the range** — extract N and M from the argument (e.g., `25-30` → IDs 0025 to 0030).
2. **For each ID in the range:**
   - Search `.planning/backlog/` for a file matching the ID prefix (e.g., `0025-*`).
   - If the file is not found in the active backlog, check `.planning/backlog/archive/`. If it's already archived or planned, **skip it silently** (log it in the summary, but do not fail).
   - Read the idea file to extract context for the plan.
   - Run the promote command — handles frontmatter update, git mv, and commit atomically:
     ```bash
     bravros backlog promote <ID>
     ```
3. **Hand off to `/plan` for each promoted item** — pass the backlog content as context. The `/plan` skill handles plan file creation, branch creation, and the `backlog: "NNNN"` bidirectional link.
4. **Output a summary** listing each item processed:
   - Promoted: backlog ID → plan ID (e.g., `0025 → plan 0177`)
   - Skipped: backlog ID — reason (e.g., `0027 — already archived`)

### Key Rules for Batch Promote

- **Never create plan files directly** — always delegate to `/plan`. Plan creation logic lives in one place.
- **Never fail on missing/archived items** — skip gracefully and include them in the summary.
- **No user interaction mid-loop** — the batch operation is autonomous; only ask questions before starting if the range is ambiguous.

---

## Marking Done (`/backlog done <number>`)

For ideas that were completed outside the normal plan flow (e.g., it was a quick fix that didn't need a full plan, or it was resolved by another change).

```bash
bravros backlog done <ID>
```

The CLI handles: `status: archived` in frontmatter → `git mv` to archive/ → commit `🧹 chore: archive backlog NNNN`.
Returns JSON: `{"id", "action": "done", "archived_path", "commit"}`

---

## Dropping an Idea (`/backlog drop <number>`)

For ideas that are no longer relevant — priorities changed, the feature was descoped, or it turned out to be unnecessary.

1. Ask the user for a brief reason (or accept one from args: `/backlog drop 3 superseded by new auth system`)
2. Run the drop command:
   ```bash
   bravros backlog drop <ID> --reason "<reason text>"
   ```

The CLI handles: `status: archived` + `reason` field in frontmatter → `git mv` to archive/ → commit `🔥 remove: drop backlog NNNN`.
Returns JSON: `{"id", "action": "drop", "archived_path", "commit", "reason"}`

---

## Critical Rules

**The backlog never implements.** This is the single most important rule. When a user says `/backlog add notifications`, you create a markdown file describing the idea — you do NOT write code, create migrations, build components, or make any implementation changes. The backlog captures intent. Implementation happens only after the idea is promoted (`/backlog promote`) → planned (`/plan`) → approved (`/plan-approved`).

The reason this matters: jumping straight to code skips the thinking that prevents wasted work. The backlog→plan→implement pipeline ensures ideas are evaluated, scoped, and broken down before anyone writes a line of code.

**Other rules:**
- **CLI-first: ALWAYS use `bravros backlog` for listing/reading backlog items** — never parse files with grep/sed/awk/cat. The CLI handles both YAML frontmatter and legacy blockquote formats correctly. The audit hook (rule 15) will block manual parsing attempts.
- **Old format migration: If you detect old blockquote-format files, suggest running `bravros backlog migrate` to convert them to YAML frontmatter before proceeding.**
- **Post-promote gate is mandatory:** `/backlog promote` → `/plan` → `/plan-review` → `/plan-approved` is a strict sequence. No implementation agent may be dispatched until `/plan-approved` has run.
- One idea per file — keep them atomic and independently promotable
- IDs are global and never reused across active and archive
- Always use `bravros backlog promote/done/drop` for lifecycle changes — never manual `git mv` or frontmatter edits. The CLI preserves history and commits atomically.
- The only output of `/backlog add` is: a `.planning/backlog/NNNN-*.md` file written to disk. No git commands.
- List, view, and archive commands are strictly read-only — no git commands, no file writes
- Never scan the entire codebase — backlog is file-based, only read `.planning/backlog/`
- When in doubt about type/size/priority, ask — don't guess
- Range promote (`promote N-M`) calls `sdlc backlog promote` per item — each gets its own atomic commit

Use $ARGUMENTS as: description (to add), number (to view), or subcommand + args.