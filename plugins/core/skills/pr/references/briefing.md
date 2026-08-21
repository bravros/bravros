# pr briefing

INTENT: ship everything (`/ship`), open the PR against the right base, hand off to review.

HARD CONSTRAINTS:
- PRs NEVER target `main` directly. Flow: `feature/* → homolog → main`. Only a PR *from* `homolog` targets `main`; a repo without a `homolog` branch falls back to `main`.
- Title: `<emoji> <type>: <description>`, **under 70 characters** — detail goes in the body.
- NEVER add AI signatures to the PR title or body — repo policy overrides any harness default footer.
- Never open a PR with uncommitted changes — `/ship` first.

BASE BRANCH:

```bash
BRANCH=$(git branch --show-current)
if [ "$BRANCH" = "homolog" ]; then BASE=main
elif git show-ref --quiet refs/heads/homolog || git show-ref --quiet refs/remotes/origin/homolog; then BASE=homolog
else BASE=main; fi
```

If behind `origin/$BASE`: rebase onto it and `git push --force-with-lease` before opening.
Rebase conflicts — interactive: ask (resolve now vs open as-is); autonomous: open as-is and say so.

CREATE: `gh pr create --base "$BASE" --title "<emoji> <type>: <title>" --body …` with sections Summary / Changes / Technical Notes / Test Plan / References. Context comes from the commits and the `.planning/` plan file (if any) — not from re-reading the codebase. Show the URL.

HANDOFF (the routing IS the contract):
- **Autonomous** (`.planning/.auto-*-lock` glob matches, or `--auto` in `$ARGUMENTS`): print `STATUS: pr-created. PR: #<n>. NEXT: review` and return — the pipeline owns the review trigger.
- **Interactive:** always invoke `Skill({skill: "pr-review"})` — no asking, no detection. A just-created PR cannot already have a review. Re-reviews later are `/pr-review` on its own, never `/pr`.
- **The handoff survives interruptions.** The gap between `gh pr create` and the review trigger is where this silently breaks: a user question, a failed tool call, or a new instruction lands mid-turn, gets answered, and the handoff is never reached — leaving the user to type `/pr-review` themselves, which is exactly what `/pr` exists to avoid. Treat an unrun handoff as unfinished work and complete it before reporting done.
