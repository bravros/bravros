# pr briefing

INTENT: ship everything (`/ship`), open the PR against the right base, hand off to review.

HARD CONSTRAINTS:
- PRs NEVER target `main` directly. Flow: `feature/* → homolog → main`. Only a PR *from* `homolog` targets `main`; a repo without a `homolog` branch falls back to `main`.
- Title: `<emoji> <type>: <description>`, **under 70 characters** — detail goes in the body.
- NEVER add AI signatures to the PR title or body — repo policy overrides any harness default footer. Check the repo's visibility first (`gh repo view --json isPrivate -q .isPrivate`) and grep the body file for `Generated with`, `Co-Authored-By`, `🤖` before creating — on a public repo (`false`) a leaked footer is visible to everyone, not just to the operator.
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

CREATE — the body is a file written in a **previous** tool call, never inline:

1. **Write the body with the Write tool** to an absolute path — default `<scratchpad>/pr-body-<branch-slug>.md` (the session scratchpad directory named in the system prompt); fall back to `<repo>/.planning/pr-body-<branch-slug>.md` **only when no scratchpad exists**, and delete it after the PR opens — `.planning/` is not gitignored and `/recon` commits `.planning/` wholesale, so a body left there ships in the next plan commit — with sections Summary / Changes / Technical Notes / Test Plan / References. Context comes from the commits and the `.planning/` plan file (if any) — not from re-reading the codebase. Re-read the file and remove any AI-attribution footer a harness may have appended.
2. `gh pr create --base "$BASE" --title "<emoji> <type>: <title>" --body-file /abs/path/pr-body-<slug>.md` as its own Bash call. Show the URL.

**Why two calls.** The `bravros police` PreToolUse hook reads and validates the body file *before* the command executes. A heredoc, `cat > body.md && gh pr create --body-file body.md`, or `cp … && gh pr create` in one command means the file does not exist yet at validation time, and the hook blocks it as an unreadable body — three consecutive blocks on paylog #2053 (2026-09-11 06:53) before the bare form went through. `--body "…"` inline is accepted but loses multi-line formatting and re-introduces the bare-`#N` autolink risk; prefer the file.

HANDOFF (the routing IS the contract):
- **Autonomous** (`.planning/.auto-*-lock` glob matches, or `--auto` in `$ARGUMENTS`): print `STATUS: pr-created. PR: #<n>. NEXT: review` and return — the pipeline owns the review trigger.
- **Interactive:** always invoke `Skill({skill: "pr-review"})` — no asking, no detection. A just-created PR cannot already have a review. Re-reviews later are `/pr-review` on its own, never `/pr`.
- **The handoff survives interruptions.** The gap between `gh pr create` and the review trigger is where this silently breaks: a user question, a failed tool call, or a new instruction lands mid-turn, gets answered, and the handoff is never reached — leaving the user to type `/pr-review` themselves, which is exactly what `/pr` exists to avoid. Treat an unrun handoff as unfinished work and complete it before reporting done.
