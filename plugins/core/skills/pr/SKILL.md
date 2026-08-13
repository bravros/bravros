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
- Never open a PR with uncommitted changes (`/ship` first).

BASE BRANCH:
`homolog` if present (or `main` if current is `homolog` / missing `homolog`). Rebase if behind.

CREATE:
`gh pr create --base "$BASE" --title "<emoji> <type>: <title>" --body …` with Summary, Changes, Technical Notes, Test Plan, References.

HANDOFF:
- **Autonomous**: Output `STATUS: pr-created. PR: #<n>. NEXT: review`.
- **Interactive**: Invoke `Skill({skill: "pr-review"})`.
