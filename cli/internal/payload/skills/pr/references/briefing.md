# pr briefing

INTENT: ship everything (`/commit-push`), open the PR against the right base, hand off to review.

HARD CONSTRAINTS:
- In a PR-gated repo, feature PRs NEVER target `main`. Flow: `feature/* → homolog → main`. Only a PR *from* the staging branch targets `main`. A direct-main repo (`bravros config get police.direct_main` prints `true`) has no staging branch — `main` is the expected base.
- Title: `<emoji> <type>: <description>`, **under 70 characters** — detail goes in the body.
- NEVER add AI signatures to the PR title or body — repo policy overrides any harness default footer. Check the repo's visibility first (`gh repo view --json isPrivate -q .isPrivate`) and never write `Generated with`, `Co-Authored-By`, or `🤖` into the body (a read-only grep before creating is fine; do not rewrite the file with `sed`/`perl`) — on a public repo (`false`) a leaked footer is visible to everyone, not just to the operator.
- Never open a PR with uncommitted changes — `/commit-push` first.

BASE BRANCH:

```bash
BRANCH=$(git branch --show-current)
STAGING=$(bravros config get staging_branch)
if [ "$(bravros config get police.direct_main 2>/dev/null)" = true ]; then BASE=main
elif [ "$BRANCH" = "$STAGING" ]; then BASE=main
else BASE=$STAGING; fi
```

If behind `origin/$BASE`: rebase onto it and `git push --force-with-lease` before opening. **This rewrites the remote branch** — never do it when `$BRANCH` is `main`, `master`, the staging branch, or in `permanent_branches` (`bravros config get permanent_branches`); open as-is instead.
Rebase conflicts — interactive: ask (resolve now vs open as-is); autonomous: open as-is and say so.

CREATE — the body is a file written in a **previous** tool call, never inline:

1. **Write the body with the Write tool** to an absolute path — default `<scratchpad>/pr-body-<branch-slug>.md` (the session scratchpad directory named in the system prompt); fall back to `<repo>/.planning/pr-body-<branch-slug>.md` **only when no scratchpad exists**, and delete it after the PR opens — `.planning/` is not gitignored and `/recon` commits `.planning/` wholesale, so a body left there ships in the next plan commit — with sections Summary / Changes / Technical Notes / Test Plan / References. Context comes from the commits and the `.planning/` plan file (if any) — not from re-reading the codebase. Use a literal absolute path (no `$VAR`, no `~`, no relative path, no `cp` from a scratch file). Never write an attribution line — none to strip afterwards.
2. `gh pr create --base "$BASE" --title "<emoji> <type>: <title>" --body-file /abs/path/pr-body-<slug>.md` as its own Bash call — the literal path from step 1, never a variable. Show the URL.

**Why two calls.** The `bravros police` PreToolUse hook reads and validates the body file *before* the command executes. A heredoc, `cat > body.md && gh pr create --body-file body.md`, or `cp … && gh pr create` in one command means the file does not exist yet at validation time, and the hook blocks it as an unreadable body — three consecutive blocks on paylog #2053 (2026-09-11 06:53) before the bare form went through. `--body "…"` inline is accepted but loses multi-line formatting and re-introduces the bare-`#N` autolink risk; prefer the file.

HANDOFF (the routing IS the contract):
- **Autonomous** (`.planning/.auto-*-lock` glob matches, or `--auto` in `$ARGUMENTS`): print `STATUS: pr-created. PR: #<n>. NEXT: review` and return — the pipeline owns the review trigger.
- **Interactive:** always invoke `Skill({skill: "pr-review"})` — no asking, no detection. A just-created PR cannot already have a review. Re-reviews later are `/pr-review` on its own, never `/pr`.
- **The handoff survives interruptions.** The gap between `gh pr create` and the review trigger is where this silently breaks: a user question, a failed tool call, or a new instruction lands mid-turn, gets answered, and the handoff is never reached — leaving the user to type `/pr-review` themselves, which is exactly what `/pr` exists to avoid. Treat an unrun handoff as unfinished work and complete it before reporting done.
