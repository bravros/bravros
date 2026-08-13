# Serial merge chain — conflict recovery & per-merge closeout

Detail for SKILL.md Flow step 2. Lock/merge/verify sequence itself: `skills/shared/merge-flow.md`.

## Conflict at a PR's turn

FREE TREE ONLY — under a live operator suite, recover in an isolated worktree or PARK the PR
(record it) and continue the chain.

```bash
gh pr checkout <n>
git merge origin/"$STAGING"        # bring staging INTO the PR branch
```

- Resolve KEEPING BOTH siblings' changes — never drop an earlier-merged sibling's edit — then
  re-run the touched area's tests with the project's own runner.
- If it cannot be cleanly resolved and greened, the tree MUST be left clean before the next
  PR: git refuses to switch branches mid-merge, so an un-aborted merge strands the whole
  batch. `git merge --abort && git checkout "$STAGING"`, PARK the PR, move on.
- Push only on a clean, green resolution — `git push origin HEAD`, no `--force` (a fork PR's
  head lives on the fork remote, not origin).
- `.planning/`-only conflicts → `--theirs` (base wins — metadata, not code). This is the one
  sanctioned narrow exception to merge-flow.md's no-`--theirs` rule, scoped to that path.

## Worktree isolation (when the main tree is busy)

Copy `vendor/`/deps + env files into the worktree — never symlink: a symlinked vendor breaks
autoload base paths, so edits silently bypass the running app.

## Per-merge closeout (order is a safety property)

```bash
gh pr merge <n> "$MERGE_FLAG"      # server-side, staging base; no --delete-branch
gh issue close <issue> --comment "Fixed in PR #<n> (merged to $STAGING)."
# ^ GitHub keyword auto-close ("fixes #N") does NOT fire on a non-default base — close explicitly.
git pull --ff-only origin "$STAGING"   # sync BEFORE any local completion commit, else the
                                       # next push is non-ff. FREE TREE ONLY.
# mark the linked backlog/plan item complete per the project's planning convention; commit + push
```

Under a live suite: do only the server-side calls (merge + issue close); defer pulls,
checkouts, and local completion commits until the tree is free.

## Sibling-test trap

Key any targeted test slice on the changed SOURCE symbols, not just each PR's own test files:
`grep -rl "<ChangedClassName>" tests/` and run those too — a behavior change (severity, retry
window, validation) frequently breaks a sibling test the PR never touched.
