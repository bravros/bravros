# finish briefing

INTENT: land this feature — merge the PR into its base, record completion in `.planning/events.jsonl`, route the promotion-to-main decision. Git/project operation only; never touches application code.

## Hard constraints

- **The main-merge decision is the operator's, always.** Ask via `AskUserQuestion`, unless the calling skill already asked this session and passed `--merge-main` / `--no-main` (the single-decision handoff — only a skill that just ran its own `AskUserQuestion`, e.g. `/address-pr`, may pass it). Never infer it from a lock file, never self-supply the flag. Nothing suppresses the decision — not `--batch`, not a lock: it is answered here or by the caller, never assumed.
- **`--merge-main` is conditional authorization, never a bypass.** Failing CI, open conflicts, or a `.planning/.auto-*-lock` (an autonomous run holds the tree) still stop the merge — the operator approved "merge if clean", not "merge regardless".
- **Never push directly to `main`** — main is reached only by merging a PR.
- **Never delete a branch that is permanent or checked out in any worktree.** This gate deleted the remote `homolog` twice (PRs #289/#290) before it existed — run the gate in `references/flow.md` verbatim before every merge, and keep its bash ARRAY an ARRAY: unquoted-string iteration breaks in zsh, which is exactly how those deletions happened.
- **Never `reset --hard` a worktree you do not own** — fast-forward it or leave it alone (`BASE_HELD_AT` handling in `references/flow.md`).
- **No automatic branch pruning or worktree teardown** — `/prune-merged` and `/worktree destroy` are manual, on purpose.

## Flow — order is a safety property: CI before merge, verify before reporting success

Executable detail, gates included, lives in [`references/flow.md`](references/flow.md) — load it before Step 4, not after.

1. **Resolve**: PR number from `gh pr view`; base is `homolog` when `origin/homolog` exists (unless already on it), else `main`. Compute `BASE_HELD_AT`. If the repo requires reviews and the PR is not `APPROVED`, confirm with the operator first.
2. **Close the plan** (events model — the `bravros finish` verb is retired): find the plan whose `pr_opened` event or legacy `pr:` frontmatter names this PR; append a `completed` event to `.planning/events.jsonl` per `.planning/CONVENTIONS.md`. Files are never renamed. Batch/aggregate PR with no plan → skip and carry on.
3. **CI**: `gh pr checks "$PR_NUMBER" --watch --fail-fast` (skip when the repo has no CI relevant to the diff). On failure ask: fix and retry / merge anyway / abort.
4. **Merge** per [`../shared/merge-flow.md`](../shared/merge-flow.md): capture pre-merge blobs → branch-delete gate → lock → `gh pr merge` → **post-merge blob-hash verification** (a feature file matching *base* instead of feature = change silently lost — ask in normal mode, `exit 1` under `--batch`) → confirm PR `state` is `MERGED` before reporting success.
5. **Sync local**: fast-forward a base held by another worktree; otherwise checkout base and reset to origin. Report every branch kept, with the reason.
6. **`bravros` repo only**: `bravros deploy` so the edited skills reach `~/.claude/skills/` (ask first in normal mode; just do it under `--batch`).
7. **main** — governed by the first two constraints. `HAS_HOMOLOG=false` (merged straight to main) → nothing to do. Otherwise fire the PT-BR decision announce, then ask *Merge to main now / Create the PR, I'll merge it myself / Later* — or obey the flag. Merge code + hard-conflict escape (never fight conflicts in place) in `references/flow.md`.
8. **Cleanup**: review-stamp self-heal sweep (a stamp authorizes ONE merge; drop stamps for non-OPEN PRs — covers stamps this run never wrote; 42 stale files across three repos before this existed), review-cache removal, PT-BR completion announce. For an aggregate homolog→main merge, offer `/after-merge`.

## Flags

- `--batch`: skip Step 3 (CI) and Step 7 (main); no `AskUserQuestion` anywhere. Used by `/auto-merge`.
- `--merge-main` / `--no-main`: the operator's answer, given in the calling skill this session. Step 7 proceeds / reports without re-asking.
