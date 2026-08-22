---
name: address-pr
core: true
description: Fetch PR review comments, implement the fixes, and push.
---

# address-pr

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: read the latest review (GitHub bot + local), fix everything, push, stamp, route the next step.

PR number: `$ARGUMENTS` if numeric, else `PR=$(gh pr view --json number -q .number)`.

## Quick Execution Summary

1. **Fetch Review**: GitHub bot comment + local `.planning/pr-reviews/${PR}-*.md`.
2. **Fix**: Apply all fixes (blockers → code issues → style → suggestions). Touch only files named in review.
3. **Push, Verify, Stamp**:
   - `/ship` with `🐛 fix: address PR #XX review feedback`
   - `gh pr checks "$PR" --watch --fail-fast > /tmp/bravros-checks-$PR.txt 2>&1` then `RC=$?` — **never pipe the gate**; `| tail` returns the pipe's status and a red build reads as success.
   - `bravros pr-review "$PR" --write-stamp` is commit-sha-keyed and safe to re-run every round: same HEAD → no-op, new HEAD → refreshes in place. No manual stamp deletion needed.
4. **Route**:
   - **⚠️ Re-review**: if blockers fixed, logic changed, test behavior modified, or security files touched -> invoke `Skill({skill: "pr-review"})`.
   - **✅ Optional**: only if style/typos/comments/simple additions -> ask single merge handoff for `/finish`.

```bash
bravros ha say --force "Correções da revisão $PR publicadas, próxima etapa pendente. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." >/dev/null 2>&1 || true
```
