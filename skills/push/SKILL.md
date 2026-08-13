---
name: push
core: true
description: Push current branch to remote with branch safety checks.
---

# push

INTENT: push the current branch to origin. Push only — no committing, no PR creation.

HARD CONSTRAINTS:
- Never push `main`/`master` directly — refuse and point to a PR from homolog. `homolog` itself IS directly pushable (plan commits, hotfixes).
- No force push unless the operator explicitly asked for one.
- Dirty working tree → stop and point to `/ship` or `/commit` first — committing is their job, not this skill's.
