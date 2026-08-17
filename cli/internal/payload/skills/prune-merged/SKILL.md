---
name: prune-merged
core: true
description: Safely prune already-merged branches (local + remote) with 7-day tombstone recovery. Manual-only — nothing auto-triggers it. Invoke via `/prune-merged`.
---

# Prune Merged Branches

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

Safely delete branches already merged to the base branch. Dual-signal merge-truth, five safety guards, 7-day tombstone refs for recovery. Full safety contract: `references/safety.md`.

## Critical Rules

- **Manual-only.** Nothing auto-triggers this skill — `/finish` and `/promote` never prune (P-0185). The ONLY entry point is a user typing `/prune-merged`, and Step 2 user review is mandatory before any `--apply`.
- **Both local and remote refs deleted** on a successful prune.
- **Worktree safety.** A branch checked out in any worktree is OFF-LIMITS — skipped in both dry-run and `--apply`, **even when already merged to main**, reported as `SKIPPED-WORKTREE (<path>)`. Prune never removes a worktree or deletes a worktree-backed branch; worktree teardown is owned solely by `bravros worktree cleanup <path>`. Details + rationale: `references/safety.md` Guard 5.
- **Protected by design.** Hard blocklist: `main`, `homolog`, `master`, `staging`, `develop`, current HEAD, open-plan branches, GitHub branch-protection rules, `.bravros.yml:branch_prune.protected`.
- **Recoverable.** Pruned branches write 7-day tombstone refs (`feat/foo` → `refs/tombstones/feat-foo`, slashes become dashes) — rejected-PR branches included, same contract.
- **Closed-PR branches are pruned by default, in the CLI.** A branch whose every PR is `CLOSED` (none open, none merged) is deliberately rejected work: the CLI reports it as `[CANDIDATE] … source=rejected` and `--apply` deletes it under the same Step 2 approval. `--exclude-rejected` holds them back for the rare run where you want that.

## Flow

1. **Dry-run:** `bravros branch prune --base <detected-base>` — lists candidates with source attribution (git/pr/pr-verified/rejected). Dry-run is the default; only `--apply` deletes.
2. **User review — MANDATORY.** Show the full output — the `[CANDIDATE]` lines (`source=rejected` ones included, they are deletions too), every `SKIPPED-WORKTREE` line, and the skip reasons — then ask "Proceed with deletion?". Never continue without an explicit yes.
3. **Apply:** `bravros branch prune --apply --base <detected-base>`. Add `--exclude-rejected` only if the user asked to hold rejected-PR branches back.
4. **Report:** deleted count (the summary breaks out rejected-PR deletions), log location (`~/.agent_config/logs/branch-prune.log`), recovery instructions.
