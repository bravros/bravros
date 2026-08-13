# local-review Briefing

INTENT: the same review `@claude` does, fully local — a FRESH zero-context subagent reviews the diff; findings are saved, posted to the PR for audit-trail parity, and routed.

HARD CONSTRAINTS:
- **The orchestrating session MUST NOT write the review itself** — the point is a reviewer with no stake in the code. Dispatch ONE `Agent` with `subagent_type: "code-reviewer"` (the named agent in `agents/code-reviewer.md` owns persona, dimensions, output shape) and `model: "sonnet"` (`"opus"` on `--deep`) — the agent is `model: inherit`, so the tier is load-bearing.
- **This skill never writes `.planning/.review-stamp-*.json`.** `bravros pr-review --write-stamp` is the ONE stamp-write authority, and it reads the *bot's* sentinel. A clean local verdict that should unlock an autonomous merge goes through the operator: `bravros pr-review unlock` in a **separate terminal** (Claude cannot mint it), then `bravros pr-review "$PR" --write-stamp`.

Flags: `<PR>` (default `gh pr view --json number -q .number`; none resolvable → STOP) · `--deep` (Opus reviewer) · `--post`/`--no-post` (default: post).

## Gather

One batch: `gh pr view "$PR" --json title,baseRefName,headRefName,author,url,body` + `gh pr diff "$PR"` + changed files. gh gotcha: `gh pr diff` has NO `--stat` — per-file stats come from `gh pr view --json files`. Empty diff → STOP.

## Dispatch

Prompt = context injection only (PR number/title, base branch, framework, plan file if any), the diff inline (>100 KB: first 80 KB + last 10 KB + `[... truncated ...]` marker), the worker hygiene block, and three repo-specific checks: plan alignment (scope creep / missing tasks), docs-sync (CLAUDE.md/README drift), blast radius via graphify when the repo has a graph (confirm each caller by reading it). Dispatch fails → retry once; still failing → save the raw error to `.planning/pr-reviews/<PR>-<TS>-error.md` and STOP.

## Parse + save

Extract the `VERDICT_BLOCK` (`verdict` / `blockers` / `warnings` / `suggestions` / `nits`). Missing or malformed → treat as `changes-requested` and report the parse failure. Stamp-worthy verdicts: `approved` and `no-new-blockers`; `changes-requested` never unlocks a merge.

Save `.planning/pr-reviews/${PR}-<TS>.md`: the frontmatter schema `/address-pr` parses (`skills/address-pr/references/fetch-review-data.md`) + the agent output verbatim. Never overwrite a prior review — timestamp collision gets a `-N` suffix. These files are committed.

## Post + route

Unless `--no-post`: `gh pr comment` headed `🤖 **Local Claude Review** (not @claude bot — generated locally via /local-review)` + model/base/short-SHA line — the header is how thread readers tell it from the Action. Post fails → warn, don't abort (the local file is already saved).

- **Autonomous** (`.planning/.auto-*-lock`): print `STATUS: local-review-result. NEXT: finish` (clean) or `NEXT: address-pr` (changes requested), stop.
- **Interactive — clean:** ask the merge question HERE (the single merge-decision handoff; only a skill that just ran its own AskUserQuestion may pass these flags): merge to homolog then main → `Skill({skill: "finish", args: "--merge-main"})` · homolog only → `--no-main` · second opinion → `/pr-review` · done.
- **Interactive — changes requested:** `/address-pr` · let me read it first · second opinion.

```bash
bravros ha say --force "Revisão local concluída, aguardando sua decisão sobre o próximo passo. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." >/dev/null 2>&1 || true
```
