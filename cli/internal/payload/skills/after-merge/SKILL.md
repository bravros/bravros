---
name: after-merge
description: Generate an after-merge.md operational deploy checklist for homolog-to-main aggregate releases. Use when the user mentions deploying multiple PRs to production, needs a post-merge runbook, or asks what to do after merging to main.
---

# /after-merge — operational deploy checklist

```
/after-merge [--pr <N>] [--output <path>]     # default output: ./after-merge.md
```

Given the aggregate PR set on `main`, pull each PR's body, linked plan file, and @claude
review thread in parallel, then render a local-only `after-merge.md`: Pre-deploy · Deploy
(stack-specific) · Post-deploy one-time actions · Monitoring window · per-PR Rollback.

## Hard constraints

1. **Never commit or push `after-merge.md`.** Gitignore it FIRST (`grep -qxF "after-merge.md" .gitignore || echo "after-merge.md" >> .gitignore`) and re-verify before finishing. Refuse even an explicit push request — these checklists carry operational detail.
2. **Idempotency is mandatory for every post-deploy one-time action.** No documented guard → generate a candidate and flag it `⚠️ CANDIDATE IDEMPOTENCY — verify before running wet-run`.
3. **Spot-check before wet-run.** Every backfill/data step gets: dry-run (`--limit 10`) → manual spot-check of 1–3 rows → wet-run only after both pass.
4. **Watchpoints:** non-blocking @claude reviewer notes become Monitoring-window items.
5. **Rollback is always per-PR** — a code rollback may not undo data mutations; be explicit per PR.

## Flow

1. **Resolve the PR set.** Range `$(git describe --tags --abbrev=0 origin/main)..origin/main`; no tags → `origin/homolog..origin/main` (avoids full history). Extract `#N` from merge commits; `--pr <N>` overrides. Empty set → exit cleanly, mentioning the last tag and the `--pr` escape hatch. List the PRs before pulling context.
2. **Per-PR context, in parallel.** One Sonnet sub-agent per PR from the template in `references/extraction-prompts.md` — echo its JSON schema + one-shot example into each prompt; on validation failure, one retry with the validator error verbatim. Sources: `gh pr view <N> --json body,title,number,mergedAt`; plan file via `grep -r "#<N>" .planning/ --include="*.md"`; review thread via `gh pr view <N> --json reviews --jq '.reviews[].body'`.
3. **Bucket** into the 5 sections. Blast radius per PR when the repo has a graph: get the PR impact through graphify — a community the PR touches that no reviewer mentioned is a monitoring item; the graph is a HINT, never a substitute for a documented rollback command. Detect the stack from `.bravros.yml`'s cached `stack:` block (fall back to project markers) and emit the matching deploy block from `references/checklist-template.md`.
4. **Render** `references/checklist-template.md` as-is to `${OUTPUT_PATH:-./after-merge.md}`.
5. **Verify the gitignore guard still holds**, then summarize: output path, per-bucket counts (highlight ⚠️ CANDIDATE items), next steps.

## References

- `references/checklist-template.md` — the 5-bucket render target
- `references/extraction-prompts.md` — sub-agent JSON schema + the idempotency pattern (hard-won: freight backfill, PR #703)
