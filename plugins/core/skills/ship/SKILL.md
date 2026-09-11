---
name: ship
core: true
description: Commit and push changes in one step with safety checks.
---

# ship

INTENT: `/commit` then `/push`, with one branch gate first. Never creates a PR.

HARD CONSTRAINTS:
- Refuse on `main`/`master` **in a PR-gated repo** — there those branches move only via PR (`homolog → main`).
  Every other branch, including `homolog`, is shippable directly.
- **Direct-main repos ship on `main` by design.** `.bravros/config.json` with `police.direct_main: true` (`bravros police direct-main on`, or a `/git-this` personal repo) has no staging branch — commit and push `main` there without ceremony. The gate is a pure binary: `bravros config get police.direct_main` prints `true` → direct-main, ship allowed; anything else (empty, an error, an older CLI reporting an unknown key) → PR-gated, refuse and point to a PR. `staging_branch` is never the discriminator — `config get staging_branch` never prints empty.
- `/commit`'s rules apply in full: emoji format, no secrets staged, no AI signatures.

Run `/commit`, then `Skill({skill: "push"})` — `/push` is the canonical push primitive.

Report one line — `✅ <emoji> <type>: <subject> — pushed to origin/<branch>` — or the relevant error.
