---
name: ship
core: true
description: Commit and push changes in one step with safety checks.
---

# ship

INTENT: `/commit` then `/push`, with one branch gate first. Never creates a PR.

HARD CONSTRAINTS:
- Refuse on `main`/`master` — those branches move only via PR (`homolog → main`).
  Every other branch, including `homolog`, is shippable directly.
- `/commit`'s rules apply in full: emoji format, no secrets staged, no AI signatures.

Run `/commit`, then `Skill({skill: "push"})` — `/push` is the canonical push primitive.

Report one line — `✅ <emoji> <type>: <subject> — pushed to origin/<branch>` — or the relevant error.
