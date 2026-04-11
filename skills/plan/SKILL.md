---
name: plan
description: >
  Create structured implementation plans with git branches and memory search.
  Use this skill whenever the user wants to plan a feature, fix, refactor, or any development task.
  Triggers on: "/plan", "plan this", "create a plan for", "let's plan", "I need to plan",
  "break this into phases", "how should we approach", "let's think about how to build",
  or any request that involves planning, phasing, or strategizing before coding.
  For worktree-based plans (isolated directory), use /plan-wt instead.
  ALWAYS use this skill before starting implementation — planning first, coding second.
---

# Plan: Create Implementation Plan & Branch

Create a plan in `.planning/` with phases, tasks, and acceptance criteria. Branch creation is deferred to `/plan-review`.

**Isolated worktree?** Use `/plan-wt` instead.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

Run `/plan` on **Opus**. This skill uses sequential thinking, multi-domain exploration, and architectural analysis — all tasks that benefit from deep reasoning. The planning phase is where quality matters most; execution can use cheaper models.

## Critical Rules

1. **Read the template first.** Read `references/plan-template.md` at Step 1. Never create a plan without it.
2. **Use sequential thinking.** Call `mcp__sequential-thinking__sequentialthinking` at Step 2.
3. **Ask when ambiguous.** Use `AskUserQuestion` if anything is unclear. (Skip in `--auto` mode.)
4. **Follow step order.** Steps build on each other.
5. **Do NOT mark `[H]`/`[S]`/`[O]`** — that is `/plan-review`'s job.
6. **Filename MUST end with `-todo.md`.**

## Task Quality Rules

- **Specific and actionable** — name the file(s) and concrete deliverable
- **Atomic** — independently executable by a subagent; no implicit dependencies within the same phase
- **Right-sized** — 2-4 tasks for small changes, 4-8 per phase for features, split if >10

## Backlog Integration

If `$ARGUMENTS` is a number (e.g., `/plan 3`), it refers to a backlog idea:

1. `bravros backlog --archive --format table` to find the idea
2. Read the backlog file — use its content as plan Context
3. Add `backlog: "NNNN"` to plan frontmatter
4. After plan creation: set backlog `status: planned`, `plan: "PLAN_ID"`, `git mv` to archive/

## Step 1/4: Parallel Data Gather

```bash
echo "📋 plan [1/4] parallel data gather — bravros meta + template + pull"
```

Run **simultaneously**:

1. `~/.claude/bin/bravros meta --reserve` — project, base_branch, git_remote, next_num, backlog_next as JSON (atomic ID reservation included)
2. Read `references/plan-template.md` — plan format reference
3. `~/.claude/bin/bravros branch create --checkout-only` — sync base branch
4. `~/.claude/bin/bravros detect-stack --versions` — auto-detect tech stack for the `## Tech Stack Versions` section

**Auto-mode:** If `$ARGUMENTS` contains `--auto`, strip it and skip all `AskUserQuestion` calls.

## Step 2/4: Explore + Sequential Thinking

```bash
echo "📋 plan [2/4] code exploration + sequential thinking"
```

**Explore agents** (before sequential thinking):
- Launch 1-3 Explore agents per affected domain — run simultaneously. Always set `model: "sonnet"` on these Explore agent calls.
- **Skip Explore** when: description is < 20 words and mentions a single file, or task is config/styling only
- If description mentions "old system" or "migration from", use `AskUserQuestion` to confirm the source project path (skip in `--auto`)

**Sequential thinking** — MANDATORY `mcp__sequential-thinking__sequentialthinking`:
- Break into phases with acceptance criteria and dependencies
- Body structure: Goal, Non-Goals, Context (bullet list), Tech Stack Versions, Phases, Acceptance
- Each phase: **Touches** + **Tasks** + **Verify** (3 fields only)
- Every plan includes test/verification tasks
- Do NOT assign `[H]`/`[S]`/`[O]` — `/plan-review` handles that
- 3-6 thoughts is enough for most plans

## Step 3/4: Create Plan File

```bash
echo "📋 plan [3/4] creating plan file"
```

Write the plan using the v2 frontmatter format from the template. Key fields:

```yaml
id: "NNNN"
type: feat          # feat|fix|refactor|test|style|chore|docs|hotfix
status: todo
project: <from sdlc meta>
branch: <type>/<short-description>
base: <from sdlc meta base_branch>
backlog: null       # or backlog ID if promoted
```

### Auto-fill Tech Stack Versions

Use the `detect-stack --versions` output from Step 1 to populate the `## Tech Stack Versions` section. Supplement with lockfile details:

1. **From `detect-stack`:** framework, language, test runner
2. **From lockfiles** (read `composer.lock`, `package-lock.json`, `go.sum`, etc.): extract exact versions of key packages (UI framework, ORM, auth, etc.)
3. **Deployment status:** Check if the project has production data (existing migrations count, CI/CD presence) to note whether migrations must be additive or `migrate:fresh` is safe

This section helps workers follow framework best practices and enables Context7 to fetch version-accurate documentation. See plan-template.md for format examples.

Commit:
```bash
~/.claude/bin/bravros commit "📋 plan: add NNNN-<type>-<description>"
```

## Step 4/4: Present and Next Step

```bash
echo "📋 plan [4/4] presenting plan and next step"
command -v code &>/dev/null && code "$PWD" && code "$PWD/$PLAN_FILE"
```

Output summary:
```
Plan created!
Plan:   .planning/NNNN-type-description-todo.md
Branch: type/description (created by /plan-review)
Phases: N phases, X tasks
```

### Mode-Aware Completion

Check for autonomous mode:
```bash
if [ -f ".planning/.auto-pr-lock" ]; then
  # Autonomous mode — return structured plain text (no AskUserQuestion)
  echo "STATUS: plan-ready. NEXT: plan-review"
else
  # Interactive mode — use AskUserQuestion with options
fi
```

**Normal mode (no lock file):** `AskUserQuestion` — "Plan ready. Run /plan-review now?"
- Option 1: "I'll review in VS Code first"
- Option 2: "Make edits now"
- Option 3: "Looks good — run /plan-review"

**Auto mode (lock file present):** Output `STATUS: plan-ready. NEXT: plan-review` and return.

## Completion Flow

```
/plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish
```

## Flags

- `--auto`: Skip all prompts. Used by `/auto-pr` and `/auto-pr-wt`.
