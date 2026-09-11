---
name: auto-pr
core: true
description: Fully autonomous SDLC pipeline — plan to PR, zero user intervention. Invoke via /auto-pr.
---

# /auto-pr — plan → orchestrate → PR → review loop, autonomously

INTENT: one command, one merge-ready PR. Stages delegate to `/recon` (which reviews inline) → `/orchestrate` → `/pr` → review loop, all with `--auto`.

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

## Key Constraints & Execution Summary

1. **Only runs when explicitly typed `/auto-pr`.**
2. **Zero user questions.** Compact and continue on context pressure.
3. **NEVER merge to main.** `/promote` with out-of-band token is the only path — enforced, not promised: the `.auto-pr-lock` from step 4 also closes the `bravros police` staging lane, so the hook blocks any main merge from this run.
4. **Lock before Stage 1:** `bravros autopr preflight --skill auto-pr`.
5. **Review loop sentinel:** Uses `BRAVROS-VERDICT: approved` or `BRAVROS-VERDICT: changes-requested`.
6. **Worktree isolation:** Refer to [worktree-mode.md](references/worktree-mode.md).
