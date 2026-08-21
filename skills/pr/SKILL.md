---
name: pr
category: sdlc
core: true
description: Create a Pull Request with plan context and base branch detection.
---

# pr

INTENT: ship everything (`/ship`), open the PR against the right base, hand off to review.

> [!IMPORTANT]
> Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

HARD CONSTRAINTS:
- PRs NEVER target `main` directly (`feature/* → homolog → main`).
- Title: `<emoji> <type>: <description>`, **under 70 characters**.
- NEVER add AI signatures to title or body.
- **NEVER write a bare `#N` in the body except for an issue/PR you mean to link.** GitHub
  autolinks it and stamps a cross-reference onto that issue's timeline; a "finding #3" reference
  silently spams an unrelated old issue. Write `finding 3` or backtick it.
- Never open a PR with uncommitted changes (`/ship` first).
- **Creating the PR is not the end of the task — the review trigger is.** `/pr` is one unit of
  work that finishes at HANDOFF below. The user says `/pr`, never `/pr` *and* `/pr-review`.
  Stopping after `gh pr create` leaves the job half done, so do not report success until the
  handoff has run. If something interrupts the turn between create and handoff — a question, a
  tool failure, a new instruction — resume the handoff before answering anything else.

BASE BRANCH:
`homolog` if present (or `main` if current is `homolog` / missing `homolog`). Rebase if behind.

CREATE:
`gh pr create --base "$BASE" --title "<emoji> <type>: <title>" --body …` with Summary, Changes, Technical Notes, Test Plan, References.

HANDOFF (mandatory final step — the routing IS the contract):
- **Autonomous**: Output `STATUS: pr-created. PR: #<n>. NEXT: review`. The pipeline owns the
  trigger from there.
- **Interactive**: Invoke `Skill({skill: "pr-review"})` immediately. **No asking, no detection,
  no "want me to?"** — a just-created PR cannot already have a review, so there is nothing to
  decide. (Re-reviewing an existing PR later is `/pr-review` on its own, never `/pr`.)
