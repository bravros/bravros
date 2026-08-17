---
name: finish
core: true
description: Complete a feature — merge the approved PR, record plan completion, route the homolog→main decision. Use on `/finish` or "finish the feature".
---

# finish

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: land this feature — merge the PR into its base, record completion in `.planning/events.jsonl`, route the promotion-to-main decision. Git/project operation only; never touches application code.

## Quick Summary

1. **Resolve PR & Base**: Determine PR and target base (`homolog` or `main`), then drop a **stale review stamp** (Step 1b) — after a multi-round `/address-pr` it still names round 1's commit.
2. **Close Plan**: Record `completed` event in `.planning/events.jsonl`.
3. **CI Check**: `gh pr checks --watch --fail-fast` **redirected to a file**, then `RC=$?` — never piped. Then the readiness gate: merge only at `mergeStateStatus: CLEAN`.
4. **Merge & Verify**: Execute merge gate and post-merge blob verification.
5. **Sync & Clean**: Fast-forward local branches and sweep review stamps.
6. **Main Route**: Route homolog→main decision with operator confirmation — the main PR repeats step 3 in full.

Refer to [`references/flow.md`](references/flow.md) for full shell script flow details. Its bash
is copy-paste code, not illustration: a shell-trap table, the stamp-freshness block, the CI and
readiness gates, and the blob verification all have to run **verbatim**.
