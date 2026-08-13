---
name: local-review
description: Run a local PR review without the @claude GitHub Action.
---

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

# local-review

INTENT: the same review `@claude` does, fully local — a FRESH zero-context subagent reviews the diff; findings are saved, posted to the PR for audit-trail parity, and routed.

HARD CONSTRAINTS:
- **The orchestrating session MUST NOT write the review itself** — dispatch ONE `Agent` (`code-reviewer`).
- **This skill never writes `.planning/.review-stamp-*.json`.** `bravros pr-review --write-stamp` is the ONE stamp-write authority.

Flags: `<PR>` · `--deep` · `--post`/`--no-post`.

## Overview

1. **Gather**: `gh pr view "$PR"` + `gh pr diff "$PR"` + file stats.
2. **Dispatch**: Send diff and repo checks to subagent (`sonnet` / `opus` on `--deep`).
3. **Parse + save**: Extract verdict block and save `.planning/pr-reviews/${PR}-<TS>.md`.
4. **Post + route**: Comment on PR (unless `--no-post`) and route to `/finish` or `/address-pr`.
