# cli/internal/branch — Branch Lifecycle Package

This package implements the branch lifecycle helpers backing `bravros branch prune`
(the engine behind the manual-only `/prune-merged` skill — the legacy
`bravros prune-merged-branches` verb is retired).
The core entry point is `PruneBranch(branch string, opts PruneOpts) PruneDecision`.

## Prune invariants

The correctness contract of `PruneBranch()` is documented in `prune_invariants.md` with 9
numbered invariants (I1–I9). Each invariant has a corresponding test function in
`prune_invariants_test.go`.

**Hard rule: any change to `prune.go` MUST verify all 9 invariants still hold.**

```bash
go test ./cli/internal/branch/... -run TestInvariant
```

## Key files

| File | Purpose |
|------|---------|
| `prune.go` | `PruneBranch()`, `IsMergedWithIndex()`, `IsProtected()`, `WorktreeBranches()`, `WriteTombstone()`, `GCTombstones()`, `ListAllBranches()` |
| `prindex.go` | `PRIndex` (one batched `gh api --paginate` PR fetch per run), `ClassifyPRs()`, `ProtectedBranchSet()`, `SyncRemoteRefs()` |
| `prune_test.go` | 30+ table-driven unit tests (pre-existing) |
| `prindex_test.go` | PR-index parsing, PR-state classification, stray-tip discrimination |
| `prune_invariants.md` | 9 invariants — spec, enforcement location, test reference, rationale |
| `prune_invariants_test.go` | `TestInvariant_I1_*` … `TestInvariant_I9_*` property tests |

## Batched GitHub lookups (B-0348)

Everything that needs the network is fetched **once per run** and injected through
`PruneOpts`, never once per branch. Three fields carry it:

| `PruneOpts` field | Fetched by | Fallback when absent |
|---|---|---|
| `PRIndex` | `BuildPRIndex()` — `gh api --paginate .../pulls?state=all` | per-branch `mergedPRViaGH`; PR-state classification disabled entirely |
| `ProtectedRemote` | `ProtectedBranchSet()` — `gh api .../branches?protected=true` | per-branch `IsProtectedOnGitHub` (nil map means "not fetched"; an **empty non-nil** map means "fetched, nothing protected") |
| `RemoteSynced` | `SyncRemoteRefs()` — `git remote prune origin` | per-branch call, as before |

`PRIndex.Available()` is load-bearing, not cosmetic: a branch missing from the index only
means "no PR" when the fetch actually succeeded. `ParsePRIndex` marks any incomplete parse
UNAVAILABLE for exactly this reason, and `BuildPRIndex` uses REST `--paginate` rather than
`gh pr list --limit N` because the latter truncates silently.

## Guard order in PruneBranch

Guards execute in this order; the first match causes a skip:

1. **I2** Protected branch name (local config + well-known set)
2. **I2** GitHub branch protection (best-effort network call)
3. **I1** Current HEAD
4. **I5** Open plan reference in `.planning/`
5. **I6** Active-command marker in `/tmp/agent-audit-*/`
6. **I8** Active worktree — UNCONDITIONAL (no `HasLocal` gate) and fail-closed: the branch → path
   set is parsed ONCE via `WorktreeBranches()` in `runBranchPrune` and injected through
   `PruneOpts.WorktreeBranches`; a listing error aborts the whole run. Skipped branches are
   reported `SKIPPED-WORKTREE`; teardown is owned by `/worktree destroy`
   (`bravros worktree cleanup <path>`)
7. **I4** Merge OR-gate (git-merged OR gh-PR-merged-and-SHA-ancestor-and-tip-unmoved)
8. **I9** PR-state classification of the refusals — `rejected` (every PR CLOSED) continues to
   deletion by default; `in-flight` / `stray-tip` / `no-pr` / `unknown` are kept
9. **I3** Tombstone write (must succeed before deletion)
10. **I7** Local deletion, Remote deletion (independent)

## 4-iteration fix chain

Commits motivating the guard hardening:
- `d9d47f02` — initial prune implementation
- `6f4c720a` — PR-path SHA-ancestor verification (I4)
- `99721c4e` — open-plan blocking (I5)
- `c2e27c78` — local+remote independence (I7)
- `95f1ecda` — worktree stash on apply (I8, superseded)
- worktree made OFF-LIMITS (I8 rewrite): prune never removes a worktree or deletes a worktree-backed branch in any mode — owned solely by the `/worktree` skill after the force-removal regression nuked an in-use worktree post-`/finish`
- P-0185 — Guard 6 hardened: worktree set parsed ONCE (`WorktreeBranches()`), consulted
  unconditionally (the `HasLocal` gate that let remote-only misclassifications bypass it is gone),
  and fail-closed (a listing error aborts the run instead of failing open)
