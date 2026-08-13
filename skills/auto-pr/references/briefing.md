# /auto-pr — plan → orchestrate → PR → review loop, autonomously

INTENT: one command, one merge-ready PR. Stages delegate to `/plan` (which reviews inline) →
`/orchestrate` → `/pr` → review loop, all with `--auto`.

## Hard constraints

1. **Only runs when the user EXPLICITLY typed /auto-pr.** Never substitute it for an interactive skill.
2. **Zero ask_question.** Compact and continue on context pressure; the pipeline must complete.
3. **NEVER merge to main.** `/promote` with its out-of-band token is the only path. Stop after the PR unless `--auto-merge` was passed.
4. **STATUS lines are breadcrumbs, not exits** (B-0173) — as a subagent, run every remaining stage locally in one uninterrupted invocation; the parent sends no continuation messages.
5. **Marker-less review approval writes NO stamp — by design.** Do not retry. Surface the escape hatch: operator runs `bravros pr-review unlock` from a separate terminal (this session cannot mint the token), then `bravros pr-review "$PR_NUM" --write-stamp`.
6. Max 3 review cycles; max 2 fix rounds per phase; commit after every stage with explicit file lists (never `git add .`).
7. `MERGE_READY = Green Gate AND Quality Sweep AND Acceptance Verify` — acceptance is checked by a **fresh** `acceptance-verifier` agent per [`../shared/acceptance-verify.md`](../shared/acceptance-verify.md); a second `rejected` is a hard stop; no `## Acceptance` in the plan → `unverifiable`, print the LOUD warning and halt uncertified.

## Step 0 — lock (before Stage 1)

```bash
bravros autopr force-clear --stale-after 21600   # clear a crashed run's lock (>6h)
bravros autopr set-lock --skill auto-pr
```

The lock persists until the user clears it from a separate terminal. Opt-in verify suite
(`.bravros.yml` `features.extra.verify_suite: true`): baseline per [`../shared/verify-suite.md`](../shared/verify-suite.md) Step 0, reconcile after execution.

## Flags

`--from <stage>` · `--no-review` · `--max-cycles N` · `--worktree` (isolated worktree → `references/worktree-mode.md`) · `--no-install` · `--keep-worktree` · `--auto-merge` (manual escape hatch only).

## Review loop

After `/pr`, run the trigger → wait → fix loop per `references/review-loop.md` — its sentinel block is byte-exact and load-bearing. Then post the final report (READY/BLOCKED with blockers verbatim) and announce:

```bash
# <!-- announce-template: "Fluxo automático finalizado. Revisão pronta no repositório. Projeto {PROJECT}." -->
bash ~/.bravros/scripts/announce.sh "Fluxo automático finalizado. Revisão pronta no repositório. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```
