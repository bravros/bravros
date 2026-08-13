---
name: auto-pr
core: true
description: Fully autonomous SDLC pipeline — plan to PR, zero user intervention. Invoke via /auto-pr.
---

# /auto-pr — plan → orchestrate → PR → review loop, autonomously

INTENT: one command, one merge-ready PR. Stages delegate to `/plan` (which reviews inline) → `/orchestrate` → `/pr` → review loop, all with `--auto`.

Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/auto-pr/references/briefing.md) on demand for detailed context and instructions.

## Key Constraints & Execution Summary

1. **Only runs when explicitly typed `/auto-pr`.**
2. **Zero user questions.** Compact and continue on context pressure.
3. **NEVER merge to main.** `/promote` with out-of-band token is the only path.
4. **Lock before Stage 1:** `bravros autopr force-clear --stale-after 21600 && bravros autopr set-lock --skill auto-pr`.
5. **Review loop sentinel:** Uses `BRAVROS-VERDICT: approved` or `BRAVROS-VERDICT: changes-requested`.
6. **Worktree isolation:** Refer to [worktree-mode.md](file:///Users/skaisser/Sites/bravros/skills/auto-pr/references/worktree-mode.md).
