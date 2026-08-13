# Review data — local source format, merging, priorities

## Local review files — `.planning/pr-reviews/<PR>-*.md`

Written by `/local-review`. Frontmatter `/address-pr` parses:

```yaml
---
pr: 42
reviewed_at: 2026-04-21T14:23
source: local-review
reviewer_model: sonnet
verdict: approved | changes-requested | no-new-blockers
blockers: 1
warnings: 2
suggestions: 3
nits: 0
commit_sha: abc123...
base_ref: homolog
---
```

Multiple files per PR = re-review cycles; the **most recent timestamp is authoritative**, earlier
files are historical.

## Merging GitHub + local

- **Dedupe by `file:line`** — same location in both? Keep the higher severity, note the agreement.
- **Surface disagreements** — GH clean but local flags a blocker (or vice versa): show BOTH
  verdicts so the operator can adjudicate.
- **A human reviewer overrides the bot** when they conflict.
- Report counts by source: `Fixed: 3 GH + 2 local (1 overlap)`.

## Fix priority order

1. **Blockers** — changes requested, failing checks, security concerns
2. **Code issues** — bugs, logic errors, missing validation, edge cases
3. **Style/convention** — naming, formatting, pattern adherence
4. **Suggestions** — optional improvements
5. **Questions** — respond on the PR, don't code

Approved reviews can still carry inline comments that must be addressed — parse them regardless
of verdict.
