---
name: resume
description: >
  Resume a plan after context limit, crash, or session break. Detects completed
  phases, summarizes progress, and continues execution from where it stopped.
  Use this skill whenever the user says "/resume", "continue the plan",
  "pick up where we left off", "resume plan", "context ran out", "ran out of context",
  "continue from where I stopped", or any request to resume an interrupted plan.
  Also triggers on "what was I working on", "plan status", "re-enter plan",
  "where did we stop", or returning to an in-progress plan after a break.
---

# Resume: Continue Plan After Interruption

Pick up exactly where you left off. Reads the plan, detects progress, summarizes what's done, and continues execution — without re-reading completed phases.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

**Why this exists:** When a session is interrupted (crash, break, or context exhaustion on very large plans), running `/plan-approved` again works but wastes context re-analyzing completed phases. `/resume` is the fast re-entry: status report → lean context → continue. With 1M context, most plans complete without needing resume — but it's still essential for session breaks and crashes.

**Speed target:** Status report within 15 seconds, execution resumed within 30 seconds. The user said "resume" — that IS the confirmation. Don't ask again unless they passed `--status`.

## When to Use

- Context limit hit mid-execution
- New session after a break
- Crash or terminal disconnect
- "What was I working on?" — even just for status (use `--status` flag)

## Process

### Step 1: Find Active Plan

```bash
~/.claude/bin/bravros meta
```

Returns `plan_file`, `branch`, `base_branch`, `project`, `team`. If `$ARGUMENTS` is a path or number, use that instead.

**If no active plan found:** Check `.planning/` for any in-progress plans. If none, tell the user there's nothing to resume.

### Step 2: Read Plan + Search Memory

Read the full plan file. Search memory for notes from previous sessions on this plan.

### Step 3: Analyze Progress

Scan ALL phases and classify each:

| Status | Detection | Action |
|--------|-----------|--------|
| ✅ Completed | All tasks `[x]` | Skip — don't load into context |
| 🔶 Partial | Mix of `[x]` and `[ ]` | Resume from first `[ ]` |
| ⬜ Pending | All tasks `[ ]` | Execute normally |

Extract:
- **Completed phases**: count + names (collapsed summary only)
- **Current phase**: name + which tasks are done vs remaining
- **Remaining phases**: count + names
- **Total progress**: X/Y tasks, N/M phases
- **Last activity**: most recent `[x]` timestamp
- **Execution Strategy**: re-read the strategy section for remaining rounds

**Integrity check:** For the current (partial) phase, verify that files referenced by `[x]` tasks actually exist on disk. If a task is marked `[x]` but its output file is missing (e.g., branch was reset), flag it in the status report and re-queue it.

### Step 4: Status Report

Display a clear summary:

```
🔄 Plan Resume: NNNN-type-description
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Progress: X/Y tasks completed (N/M phases)
Last activity: YYYY-MM-DDTHH:MM

✅ Phase 1: Foundation (8/8 tasks)
✅ Phase 2: Service Layer (5/5 tasks)
🔶 Phase 3: API Integration (2/6 tasks) ← RESUME HERE
   ✓ 3.1 Create endpoint
   ✓ 3.2 Add validation
   → 3.3 Wire service layer        ← NEXT
   · 3.4 Error handling
   · 3.5 Rate limiting
   · 3.6 Feature tests
⬜ Phase 4: Frontend (0/4 tasks)

Branch: feat/description
```

If memory search found relevant notes from previous session, include them:
```
📝 Previous session notes:
- Decided to use FormRequest for validation (plan 0041 pattern)
- Blocker: API rate limit needs config value — check .env
```

If integrity check found missing files:
```
⚠️ Integrity: Task 3.1 marked [x] but app/Http/Controllers/FooController.php missing — re-queued
```

### Step 5: Route by Intent

**If `--status` or `-s` flag passed, OR user said "what was I working on" / "plan status" / "where did we stop":**
→ Show status report only. Use `AskUserQuestion` to ask if they want to continue execution.

**If plan has no `[x]` marks at all:**
→ Plan was never started. Tell the user and redirect to `/plan-approved`.

**Otherwise (default — user said "resume", "continue", etc.):**
→ Proceed directly to Step 6. The user's invocation IS the confirmation. No `AskUserQuestion` needed.

### Step 6: Hand Off to Execution

1. **Update frontmatter** `session: "${CLAUDE_SESSION_ID}"` (overwrite — single resume ID, not array)

2. **Build lean context** — only include:
   - Plan Context Pack section
   - Current phase (full detail)
   - Next phase (if needed for planning)
   - Execution Strategy for remaining rounds
   - Skip completed phases entirely

4. **Resume execution** using the same logic as `/plan-approved` Step 6:
   - If remaining work fits a single round → execute directly
   - If multiple rounds remain → dispatch per Execution Strategy
   - Workers get the lean context, not the full plan history

5. **Sync progress:**
   ```bash
   git add .planning/ && git commit -m "📋 plan: resumed NNNN from phase N"
   ```

### Step 7: Post-Resume Checkpoint

After completing the current phase:

Use `AskUserQuestion`:
- **"Continue to next phase"** → Loop back to dispatching next round
- **"Run /plan-check"** → All phases done, run verification
- **"Pause here"** → Commit progress, user will resume later

## Rules

- **Never re-execute completed tasks** — trust `[x]` marks (but verify files exist for partial phases)
- **Always show status report** before executing — user needs to know where things stand
- **Don't block on confirmation** — "resume" IS the intent. Only ask with `--status` flag
- **Lean context is key** — the whole point is saving context for remaining work
- **Don't collapse phases** — keep all tasks visible at all times
- **Search memory first** — previous session may have left notes about blockers or decisions
- **Timestamps are truth** — use `[x]` timestamps to reconstruct execution order
- **If plan has no [x] marks** — it was never started, redirect to `/plan-approved`

## Flags

- `--status` / `-s`: Show status report only, don't execute
- `--force` / `-f`: Legacy alias, now default behavior (resume without asking)

Use $ARGUMENTS as plan number, file path, or flags.