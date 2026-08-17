# Prune Merged: Safety Contract

## Merge Truth (accept one of)

Before deletion on the merged path, a branch must first be confirmed **merged**:

1. **Git ancestry:** `git branch --merged <base>` includes the branch, OR
2. **GitHub PR merged:** the batched PR index holds a MERGED PR for the branch, AND its
   `merge_commit_sha` is an ancestor of base, AND the branch's live tip still equals the
   `head.sha` GitHub recorded for that PR (no commits pushed after the merge)

If neither signal confirms merge, the branch is not deleted **on the merged path** — it falls
through to PR-state classification, where only the `rejected` class is deletable. Merge truth
is a **precondition**, not one of the numbered guards — the guards can still refuse a branch
even after merge is confirmed.

### Why the OR-gate exists

This repo squash-merges. A squash-merged branch's tip is **never** an ancestor of base, so
`git branch --merged` false-negatives on every cleanly merged PR; the PR signal covers them.
The `merge_commit_sha` ancestor check defends against stale/cached PR metadata — if
verification fails, the branch is NOT deleted.

## PR-State Classification (the `rejected` path — B-0348)

Every pull request in the repo is fetched **once** per run (`gh api --paginate
repos/{owner}/{repo}/pulls?state=all`) and indexed by head branch — one paged index, never
per-branch lookups. Branches the merge check refused — and only those, after all guards —
classify by PR state:

| Class | Condition | Reported as | `--apply` |
|---|---|---|---|
| `rejected` | ≥1 PR and **every** PR is `CLOSED` | `[CANDIDATE] … source=rejected (all PRs closed: #N, #M)` | **DELETED by default**, tombstone first |
| `in-flight` | any PR is `OPEN` | `[SKIP] … reason=in-flight` | kept |
| `stray-tip` | a PR is `MERGED` but the branch tip moved past the merged head | `[SKIP] … reason=stray-tip` | kept, surfaced for human review |
| `no-pr` | no PR at all | `[SKIP] … reason=no-pr (possible local-only work)` | kept — NEVER auto-deleted |

**Default-on is deliberate** (operator standing decision, 2026-08-09): plain `--apply`
deletes `rejected` branches with no extra flag. `--exclude-rejected` is the opt-out; there
is no `--include-rejected`.

**Fail-safe on an unavailable index.** If the batch fetch fails — or only partly parses —
the whole index is marked unavailable, classification is skipped, and every refused branch
reports flat `unmerged`. Nothing is ever classified `rejected` on a failed fetch. This
closes the pre-B-0348 blind spot where a transient `gh` failure on a per-branch lookup read
as "this branch has no PR".

**Why `stray-tip` is not "tip is not an ancestor of base".** Under squash-merge that is true
of *every* merged branch. The discriminator is branch tip vs the PR's recorded `head.sha`:
equal → never moved after merge (prunable); different → commits pushed afterwards that live
nowhere else (`afterpay296` / PR #334).

## Guards (in order; first hit ends the per-branch flow)

All run **before** merge truth and classification — a rejected-PR branch that is
worktree-held, protected, or attached to an open plan stays put.

1. **Protected names** — `main`, `master`, `homolog`, `staging`, `develop` (case-insensitive) + anything in `.bravros.yml:branch_prune.protected`.
2. **GitHub branch protection** — protected set fetched once per run (`gh api --paginate …/branches?protected=true`), per-branch fallback on failure. Best-effort: offline/unauthenticated silently reads "not protected" so prune works offline.
3. **Current HEAD** — never delete the checked-out branch.
4. **Open-plan ref** — any `.planning/*-{approved,reviewed,in-progress}.md` with `branch: <name>` in YAML frontmatter (frontmatter must start at byte 0; mid-file `---` fences rejected).
5. **Worktree-active — always skip.** `git worktree list --porcelain` parsed ONCE up front; a listing error aborts the whole run (fail-closed). Prune touches neither the worktree, the local branch, nor the remote ref — `SKIPPED-WORKTREE (<path>)`, even for branches already merged to main. Worktree lifecycle is owned solely by `bravros worktree cleanup <path>` (Herd unlink, TLS-cert removal, Redis-prefix flush — none of which prune knows how to do; a force-removing prune once nuked an in-use worktree right after `/finish`). After cleanup the branch is remote-only, the guard no longer applies, and the next pass prunes it normally. Raw `git worktree remove`/`rm -rf` of a registered worktree is floor-blocked; only the sanctioned teardown path sets the `BRAVROS_WORKTREE_DESTROY=1` bypass.

## Recovery

- **Within 7 days:** `git update-ref refs/heads/feat/foo refs/tombstones/feat-foo` (slash → dash in tombstone names), optionally `git push origin refs/heads/feat/foo`.
- **After 7 days:** reflog only — `git reflog | grep <branch>`, checkout the sha, recreate the branch.
- **Audit trail:** `~/.bravros/logs/branch-prune.log` — `[ISO-UTC] repo= branch= action=deleted|skipped|dry-run reason= tombstone=`.
- **GC:** `bravros branch prune --gc` deletes tombstones older than 7 days (reflog timestamp on the tombstone ref); non-blocking, failures logged.

## Dry-run semantics

There is no `--dry-run` flag — **absence of `--apply` IS the safe mode**. `bravros branch
prune --base main` only prints candidates with merge-source attribution and never touches
refs. Scope is the current repository only — invoke `/prune-merged` per-repo.
