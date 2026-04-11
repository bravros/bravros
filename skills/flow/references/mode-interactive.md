# Interactive Flow Mode

This document defines the interactive behavior for `/flow` skill — the manual, checkpoint-driven variant with AskUserQuestion pauses.

**Contrast with:**
- `mode-autonomous.md` for `/auto-pr` and `/auto-pr-wt` (zero pauses, all decisions made by Claude)
- `pipeline.md` for shared core stages

## Three Mandatory Checkpoints

The interactive flow has **exactly 3 checkpoints** where it pauses and asks the user what to do. All other stages flow automatically.

### Checkpoint A: Post-Review (after Stage 2)

**When:** `/plan-review` completes

**Display:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⏸  CHECKPOINT: Plan Review Complete
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Plan: NNNN-type-description
Phases: N phases, M tasks
Strategy: [execution mode summary]
Linear: KPG-XX (if applicable)

Plan size determines next step:
    ≤15 tasks: continue directly to /plan-approved
    >15 tasks: recommend clearing context first
```

**Use AskUserQuestion with options:**

**For small/medium plans (≤15 tasks):**
- **"Continue — run /plan-approved now"** → Proceed directly to Stage 3 in same session
- **"I want to adjust the plan"** → Let user edit plan file, then re-run `/plan-review`
- **"Stop here for now"** → Save progress, explain user can resume with `/flow --from plan-approved`

**For large plans (>15 tasks):**
- **"Clear context first"** → Explain: Run `/flow --from plan-approved` in fresh session
- **"Continue anyway"** → Proceed to Stage 3 in same session (acceptable if context < 50%)
- **"I want to adjust the plan"** → Let user edit, re-run `/plan-review`
- **"Create a worktree first"** → Suggest running `/plan-wt` instead, explain worktree isolation benefits

**Decision logic:**
- If context < 50%: always allow "Continue anyway"
- If context 50-70%: allow "Continue anyway" but warn about context
- If context > 70%: strongly recommend clearing, but still allow "Continue anyway"

### Checkpoint B: Post-Check (after Stage 4)

**When:** `/plan-check` completes

**Display:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⏸  CHECKPOINT: Plan Check Complete
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Plan: NNNN-type-description
Check result: [pass/issues found]
Issues: N issues found (if any)
```

**If plan-check found issues:**

Use AskUserQuestion:
- **"Fix issues and re-check"** → Show user the issues, let them fix manually, then re-run `/plan-check`
- **"Proceed to PR anyway"** → Continue to Stage 5 despite issues (risky, but user's choice)
- **"Stop here"** → Save progress, explain user can fix and resume with `/flow --from plan-check`

**If plan-check passed clean:**

Use AskUserQuestion:
- **"Looks good — create PR"** → Proceed directly to Stage 5 in same session
- **"Wait, I want to review first"** → Pause, let user review implementation (they browse code in their editor), then either continue to Stage 5 or stop
- **"Stop here for now"** → Save progress, user can resume with `/flow --from pr`

### Checkpoint C: Post-PR (after Stage 5)

**When:** `/pr` completes and PR is created

**Display:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⏸  CHECKPOINT: PR Created
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

PR: #NNN — title
URL: https://github.com/...
Branch: feat/description → homolog (or main)
Status: Open, awaiting review
```

Use AskUserQuestion:
- **"PR looks good — run /finish"** → Proceed to Stage 6 directly (merge PR, archive plan)
- **"Wait for review first"** → Stop here, explain user can run `/finish` later after getting feedback
- **"Needs changes"** → Stop here, explain user should make changes and then `/flow --from pr` to re-create

## Flow Between Checkpoints

**Stage 1 → Stage 2 (automatic):**
- `/plan` completes
- `/plan-review` runs automatically
- → Hit **Checkpoint A**

**Stage 2 → Stage 3 (decision point):**
- At Checkpoint A, user chooses "Continue — run /plan-approved now"
- → Stage 3 runs automatically
- → Stage 4 runs automatically
- → Hit **Checkpoint B**

**Stage 4 → Stage 5 (decision point):**
- At Checkpoint B, user chooses "Looks good — create PR" or "Proceed to PR anyway"
- → Stage 5 runs automatically
- → Hit **Checkpoint C**

**Stage 5 → Stage 6 (decision point):**
- At Checkpoint C, user chooses "PR looks good — run /finish"
- → Stage 6 runs automatically (merge, archive, Linear update)
- → Pipeline complete

## Context Break Recommendations

The interactive flow is smart about context management:

**At Checkpoint A (post-review):**
```
Context < 30%:    ✅ Full pipeline in one session — continue directly
Context 30-50%:   ⚠️ Suggest clear, but continuing works fine
Context > 50%:    ⚠️ Recommend clear before plan-approved
                     Say: "Large plan + high context. Consider running /flow --from plan-approved in a fresh session."
```

**At Checkpoint B (post-check):**
```
Context > 70%:    ⚠️ Warn: "Context approaching limit. Consider stopping here and running /flow --from pr in a fresh session."
```

**At Checkpoint C (post-PR):**
```
Context > 85%:    🔴 Warn: "Context at limit. You can stop here and run /flow --from finish in a fresh session to merge."
```

## Special Cases

### "Stop here for now" Flow

If user chooses to stop at any checkpoint:

1. **Explain the resume command:**
   ```
   You can resume later with:
   /flow --from plan-approved       ← to continue execution
   /flow --from plan-check          ← to re-verify implementation
   /flow --from pr                  ← to re-create PR
   /flow --from finish              ← to merge and archive
   ```

2. **Save state:**
   - Plan file is already saved in `.planning/`
   - All commits are on the feature branch
   - User can safely switch contexts

3. **Example:**
   ```
   You chose to stop. All changes are safe in:
   - Branch: feat/awesome-feature
   - Plan: .planning/0042-awesome-feature-todo.md

   Resume anytime with: /flow --from plan-approved
   ```

### Quick Flow Suggestion

At the start, if the description sounds small (≤3 tasks), suggest:

```
This sounds like a small task. Would you prefer:
1. /quick — Just do it, no plan overhead
2. /flow — Full pipeline (recommended for anything touching 4+ files)
```

Then use AskUserQuestion to let them choose.

### Hotfix Flow Suggestion

If user says "urgent", "hotfix", or "emergency":

```
This sounds urgent. Would you prefer:
1. /hotfix — Emergency: commit → push → PR → merge (fastest)
2. /flow — Full pipeline with review checkpoints (safer)
```

Then use AskUserQuestion to let them choose.

### Resume from Partial Progress

If `/flow --from plan-approved` detects partial progress (Phase 2/4, 8/20 tasks done):

```
Plan has partial progress (Phase 2/4, 8/20 tasks).
Consider using /resume for efficient re-entry instead of /flow.

Would you like to:
1. Use /resume (smart restart)
2. Continue with /flow --from plan-approved
3. Manual edit the plan file
```

Then hand off to `/resume` if the user chooses option 1.

## Rules for Interactive Flow

1. **All 3 checkpoints are mandatory** — never skip Checkpoint A, B, or C
2. **Use AskUserQuestion for every checkpoint** — never auto-proceed without asking
3. **Context breaks are conditional, not forced** — only needed for large plans (>15 tasks)
4. **Search memory at entry** — check past plans to inform the current one
5. **Explain resume paths clearly** — every "stop here" includes explicit next command
6. **Checkpoint pauses should feel natural** — don't add unnecessary pauses between stages
7. **If user is AFK:** Checkpoint timeouts should be long (user expected to review), but implementation details (stages 3-4) can be fast

## References

- **pipeline.md** — Core shared stages and context management
- **mode-autonomous.md** — Contrast: autonomous behavior (no checkpoints)
- **worktree-setup.md** — Worktree variant (`/plan-wt` suggestion)
