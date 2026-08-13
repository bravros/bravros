---
name: hotfix
core: true
description: Emergency hotfix deploy — commit, push homolog, PR to main, merge now. Use on `/hotfix <description>`.
---

# hotfix

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: ship an urgent production fix now, bypassing the plan workflow. Flow: commit → push/merge into homolog → PR homolog→main → merge → sync back. `$ARGUMENTS` is the description — ask if empty.

## Hard constraints

- **Running `/hotfix` IS the approval for merge-to-main** — the emergency-path exemption: no user question checkpoints between commit and merge.
- **The autopr lock is the one hard gate that remains.** Refuse if `bravros autopr status` reports lock present.
- **Merge-lock is intentionally skipped** — one emergency at a time.
- **NEVER delete the homolog branch after merge. NEVER skip the PR** — main is protected.
- If targeted tests fail, STOP and ask.

## Quick Flow Summary

1. Refuse on `main`/`master`. Strip issue ref for PR title / `Closes #42`.
2. Format files → `bravros commit "🩹 hotfix: <description>" <changed files only>`.
3. Push & merge to `homolog` → `gh pr create --base main --head homolog --title "🩹 hotfix: <description>"`.
4. Check autopr gate → `gh pr merge "$PR_NUMBER" --merge` → verify state == `MERGED`.
5. Sync `homolog` from `main` (`git checkout homolog && git pull && git fetch origin main && git merge ...`).
6. Close plan if applicable (`.planning/events.jsonl`) → `bravros commit`.
7. Announce via `bravros ha say --force ... studio`.
