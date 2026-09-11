---
name: promote
core: true
description: Fast `homolog → main` merge for committed, pushed work. Trigger — `/promote`. Requires out-of-band token minted via `bravros promote unlock` from a separate terminal — Claude cannot mint it.
---

# promote

> **CRITICAL:** Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: Promote accumulated `homolog` work to production (`homolog → main`). Calm-day merges only (`/hotfix` for incidents, `/finish` for feature completion).

## Execution Summary

1. **Pre-flight**:
   - Verify on `homolog`, working tree clean, up to date with remote.
   - Check authority token via `bravros promote status --field present`. If false, instruct operator to run `bravros promote unlock` in a non-Claude-Code terminal.
   - Run `git fetch origin main --quiet` and snapshot pre-merge main tip:
     `git update-ref refs/bravros/promote-base "$(git rev-parse origin/main)"`.
2. **PR & Merge** — PR and CI first, token check last:
   - Create PR from `"$PROMOTE_BASE..homolog"`; wait until `mergeStateStatus` is `CLEAN`.
   - **TTL check right before the merge**: `bravros promote status --field ttl_remaining`. The token is 5-minute; PR creation + CI + a review has burnt it in 3 sessions. Expired → ask for ONE re-mint (`bravros promote unlock` in a separate terminal), wait, never loop.
   - Acquire merge lock: `bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=promote --meta pr="$PR_NUMBER"`.
   - Merge PR: `gh pr merge 1234 --merge > /tmp/bravros-merge-1234.txt 2>&1` — substitute the **literal** PR number (the police hook reads raw command text; a `$VAR` there is unreadable → blocked), no `cd … &&`, no `| tail` — then verify `MERGED` state. The `bravros police` hook honours the promote token — **no second token** (`police unlock`) is needed; a `✋🏽 Police Block` here names its reason (usually expired token or `UNKNOWN` mergeability) — relay it in one line, never `gh api`/raw HTTP.
3. **Sync & Close-out**:
   - Execute close-out procedure detailed in [`references/close-out.md`](references/close-out.md).
   - Fast-forward `homolog` from `main`, push, release lock (`bravros merge-lock release`).
   - Close shipped plans, delete snapshot ref, revoke token (`bravros promote revoke`), and send PT-BR announce.
