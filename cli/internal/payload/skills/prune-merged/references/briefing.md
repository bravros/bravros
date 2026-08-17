# Prune Merged Branches: Briefing & Context

Safely delete branches already merged to the base branch. Dual-signal merge-truth, five safety guards, 7-day tombstone refs for recovery. Full safety contract: `references/safety.md`.

## Critical Rules

- **Manual-only.** Nothing auto-triggers this skill — `/finish` and `/promote` never prune (P-0185). The ONLY entry point is a user typing `/prune-merged`, and Step 2 user review is mandatory before any `--apply`.
- **Both local and remote refs deleted** on a successful prune.
- **Worktree safety.** A branch checked out in any worktree is OFF-LIMITS — skipped in both dry-run and `--apply`, **even when already merged to main**, reported as `SKIPPED-WORKTREE (<path>)`. Prune never removes a worktree or deletes a worktree-backed branch; worktree teardown is owned solely by `bravros worktree cleanup <path>`. Details + rationale: `references/safety.md` Guard 5.
- **Protected by design.** Hard blocklist: `main`, `homolog`, `master`, `staging`, `develop`, current HEAD, open-plan branches, GitHub branch-protection rules, `.bravros.yml:branch_prune.protected`.
- **Recoverable.** Pruned branches write 7-day tombstone refs (`feat/foo` → `refs/tombstones/feat-foo`, slashes become dashes) — rejected-PR branches included, same contract.
- **Closed-PR branches are pruned by default, in the CLI.** A branch whose every PR is `CLOSED` (none open, none merged) is deliberately rejected work: the CLI reports it as `[CANDIDATE] … source=rejected` and `--apply` deletes it under the same Step 2 approval. `--exclude-rejected` holds them back for the rare run where you want that.

## Safety Model (summary — authoritative detail in `references/safety.md`)

**Merge truth (OR-gate, precondition):** a branch counts as merged if git ancestry (`git branch --merged <base>`) OR a merged PR (with `merge_commit_sha` verified as an ancestor of base) confirms it. Neither → not deleted.

**Five guards, evaluated in order, first hit ends the per-branch flow:** 1 protected names · 2 GitHub branch protection (degrades silently offline) · 3 current HEAD · 4 open-plan `branch:` reference in `.planning/*-{approved,reviewed,in-progress}.md` frontmatter · 5 worktree-active (parsed once up front from `git worktree list --porcelain`; listing failure aborts the whole run fail-closed).

**PR-state classes (only for branches the merge check refused, after all guards):** `rejected` every PR CLOSED → **deletion candidate** · `in-flight` a PR is OPEN → kept · `stray-tip` a PR merged but the branch moved on → kept, surfaced · `no-pr` → kept, never auto-deleted. If the CLI can't fetch the PR index it classifies nothing — every refusal reports plain `unmerged` and no rejected branch is deleted (B-0348).

## Flow

1. **Dry-run:** `bravros branch prune --base <detected-base>` — lists candidates with source attribution (git/pr/pr-verified/rejected). Dry-run is the default; only `--apply` deletes.
2. **User review — MANDATORY.** Show the full output — the `[CANDIDATE]` lines (`source=rejected` ones included, they are deletions too), every `SKIPPED-WORKTREE` line, and the skip reasons — then ask "Proceed with deletion?". Never continue without an explicit yes.
3. **Apply:** `bravros branch prune --apply --base <detected-base>`. Add `--exclude-rejected` only if the user asked to hold rejected-PR branches back.
4. **Report:** deleted count (the summary breaks out rejected-PR deletions), log location (`~/.bravros/logs/branch-prune.log`), recovery instructions.

## Tombstone Recovery & GC

Recover within 7 days: `git update-ref refs/heads/feat/foo refs/tombstones/feat-foo` (then optionally push). Tombstones older than 7 days are GC'd by `bravros branch prune --gc`, which **also reaps orphaned review-stamps**: removes a `.planning/.review-stamp-*.json` **only** when its PR is `MERGED` or `CLOSED` — fail-closed on any `gh` error.

<!-- announce-template: "Removidas {N} ramificações obsoletas. Projeto {PROJECT}." -->
Fire after the final step when deletion count > 0:

```bash
# bash ~/.bravros/scripts/announce.sh "Removidas $DELETED_COUNT ramificações obsoletas. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```
