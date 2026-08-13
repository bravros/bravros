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
2. **PR & Merge**:
   - Create PR from `"$PROMOTE_BASE..homolog"`.
   - Acquire merge lock: `bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=promote --meta pr="$PR_NUMBER"`.
   - Merge PR: `gh pr merge "$PR_NUMBER" --merge` and verify `MERGED` state.
3. **Sync & Close-out**:
   - Execute close-out procedure detailed in [`references/close-out.md`](references/close-out.md).
   - Fast-forward `homolog` from `main`, push, release lock (`bravros merge-lock release`).
   - Close shipped plans, delete snapshot ref, revoke token (`bravros promote revoke`), and send PT-BR announce.
