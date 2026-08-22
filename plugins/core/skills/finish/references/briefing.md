# finish briefing

INTENT: land this feature — merge the PR into its base, record completion in `.planning/events.jsonl`, route the homolog→main decision. Git/project operation only; never touches application code.

## Hard constraints

- **The main-merge decision is the operator's, always.** Ask via `ask_question`, unless the calling skill already asked this session and passed `--merge-main` / `--no-main` (the single-decision handoff — only a skill that just ran its own `ask_question`, e.g. `/address-pr`, may pass it). Never infer it from a lock file, never self-supply the flag. Nothing suppresses the decision — not `--batch`, not a lock: it is answered here or by the caller, never assumed.
- **`--merge-main` is conditional authorization, never a bypass.** Failing CI, open conflicts, or a `.planning/.auto-*-lock` (an autonomous run holds the tree) still stop the merge — the operator approved "merge if clean", not "merge regardless".
- **Never push directly to `main`** — main is reached only by merging a PR.
- **Never say "promote" to the operator in Step 7, and never imply a promote token is wanted.** Step 7 merges through its own PR against `main` and consumes no token; the word is `/promote`'s trigger and sends the operator to a second terminal to mint one (afterpay #395/#396 — minted mid-merge, expired unused). Operator-facing text says **merge to main**, under a `Main merge` header; `/promote` is named only inside the *defer* option. Handed a token anyway? Say it is not needed here and carry on — never abandon a green main PR to re-enter through `/promote`.
- **Never merge a PR whose `mergeStateStatus` is not `CLEAN`.** Green checks in Step 3 are not the same fact as "GitHub says mergeable now": `UNSTABLE` means a gate is still running or failed. This applies to the homolog→main PR in Step 7 too — it is opened seconds earlier, so its checks are always `queued` at first and it needs its own full wait (PR #1922 was merged through it).
- **A review stamp recording a commit other than HEAD is stale, not a blocker.** Drop it and re-stamp from the latest verdict (Step 1b) — removing a stamp only ever removes authority, so it needs no operator sign-off. Never `rm` a stamp whose `commit_sha` equals HEAD.
- **Run the bash in `references/flow.md` verbatim.** Its trap table exists because improvising the merge verification live produced three broken versions in a row (PR #1919): zsh eats `:a` in `"$SHA:app/…"`, bare `rev-parse` prints missing paths to stdout, and a piped gate reports the pipe's exit code instead of the command's.
- **Never delete a branch that is permanent or checked out in any worktree.** This gate deleted the remote `homolog` twice (PRs #289/#290) before it existed — run the gate in `references/flow.md` verbatim before every merge, and keep its bash ARRAY an ARRAY: unquoted-string iteration breaks in zsh, which is exactly how those deletions happened.
- **Never `reset --hard` a worktree you do not own** — fast-forward it or leave it alone (`BASE_HELD_AT` handling in `references/flow.md`).
- **Never switch branches inside a linked worktree.** Each worktree is pinned to its feature branch (the operator runs several in parallel). Step 5 and the Step 7 homolog-sync must detect a linked worktree (`git rev-parse --git-dir` ≠ `--git-common-dir`) and sync via fetch / server-side push only — a `git checkout <base>` here strands the worktree on the wrong branch.
- **No automatic branch pruning or worktree teardown** — `/prune-merged` and `/worktree destroy` are manual, on purpose.

## Flow — order is a safety property: CI before merge, verify before reporting success

Executable detail, gates included, lives in [`references/flow.md`](flow.md) — load it before Step 4, not after.

1. **Resolve**: PR number from `gh pr view`; base is `homolog` when `origin/homolog` exists (unless already on it), else `main`. Compute `BASE_HELD_AT`. If the repo requires reviews and the PR is not `APPROVED`, confirm with the operator first. Then **refresh a stale review stamp** (Step 1b in `references/flow.md`) — after a multi-round `/address-pr` the stamp still names round 1's commit.
2. **Close the plan** (events model — the `bravros finish` verb is retired): find the plan whose `pr_opened` event or legacy `pr:` frontmatter names this PR; append a `completed` event to `.planning/events.jsonl` per `.planning/CONVENTIONS.md`. Files are never renamed. Batch/aggregate PR with no plan → skip and carry on.
3. **CI**: `gh pr checks "$PR_NUMBER" --watch --fail-fast` redirected to a file, then `RC=$?` — **never piped** (`| tail` reports the pipe's status, so a red build reads as `rc=0`). Skip only when the repo has no CI relevant to the diff. On failure ask: fix and retry / merge anyway / abort. Then the **readiness gate**: `mergeStateStatus` must be `CLEAN`. Both in `references/flow.md` Step 3/3b.
4. **Merge** — exact recipe in [`references/flow.md`](flow.md) Step 4: capture pre-merge blobs → branch-delete gate → lock → `gh pr merge` → **post-merge blob-hash verification** (a feature file matching *base* instead of feature = change silently lost — ask in normal mode, `exit 1` under `--batch`) → confirm PR `state` is `MERGED` before reporting success. The blob recipes are copy-paste code on purpose; `ABSENT` on both sides is the correct pass for a deleted or renamed file.
5. **Sync local**: fast-forward a base held by another worktree; in a linked worktree stay on the feature branch (fetch only); otherwise checkout base and reset to origin. Report every branch kept, with the reason.
6. **`bravros` repo only**: `bravros deploy` so the edited skills reach the host config dir's `skills/` (ask first in normal mode; just do it under `--batch`).
7. **main** — governed by the first two constraints. `HAS_HOMOLOG=false` (merged straight to main) → nothing to do. Otherwise report the main-merge scope (`origin/main..origin/homolog` — this ships every accumulated commit, not just this feature), fire the PT-BR decision announce, then ask under the `Main merge` header — never `Promote` — *Yes — merge to main now / Open the PR, I'll merge it myself / Not yet — accumulate*, all three, with "no promote token needed" stated on the merge option — or obey the flag. The main PR repeats Step 3 + 3b in full. Merge code + hard-conflict escape (never fight conflicts in place) + the Step 7 vs `/promote` boundary in `references/flow.md`.
8. **Cleanup**: review-stamp self-heal sweep (a stamp authorizes ONE merge; drop stamps for non-OPEN PRs — covers stamps this run never wrote; 42 stale files across three repos before this existed), review-cache removal, PT-BR completion announce. For an aggregate homolog→main merge, offer `/after-merge`.

## Flags

- `--batch`: skip Step 3 (CI) and Step 7 (main); no `ask_question` anywhere. Used by `/auto-merge`.
- `--merge-main` / `--no-main`: the operator's answer, given in the calling skill this session. Step 7 proceeds / reports without re-asking.
