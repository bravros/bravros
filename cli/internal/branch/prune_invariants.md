# Prune Invariants

Specification for the 9 correctness invariants of `PruneBranch()` in `prune.go`.

**Hard rule:** Any change to `prune.go` MUST verify all 9 invariants still hold.
Run: `go test ./cli/internal/branch/... -run TestInvariant`

**Enforcement references point at guards and functions, never line numbers.** Line numbers in
this file went stale silently and sent readers into the wrong function; a doc that calls itself
the correctness contract cannot do that. Cite `Guard N in PruneBranch` or the function name.

---

## I1: Current HEAD Immunity

**Statement.** `PruneBranch` never deletes the currently checked-out branch.

**Enforcement.** `prune.go`, Guard 3 in `PruneBranch` — `IsCurrentHEAD(branch)` is called after the protected-branch
check. If it returns `true`, `PruneBranch` returns immediately with `SkipReason = SkipCurrentHEAD`.
No deletion path is reached.

**Test reference.** `TestInvariant_I1_CurrentHEADImmunity` in `prune_invariants_test.go`.

**Rationale.** Git forbids deleting the currently-checked-out branch (you would lose the
working tree). The guard was hardened during the 4-iteration fix chain to run before any
merge check, so even a branch that is fully merged into main cannot be deleted while active.

---

## I2: Protected Branch Immunity

**Statement.** `PruneBranch` never deletes a branch whose name matches the permanent-branch
list (`main`, `homolog`, `staging`, `develop`, `master`, `production`) or the repo's
custom `permanent_branches` config, OR any GitHub branch-protection rule.

**Enforcement.** `prune.go`, Guard 1 in `PruneBranch` — `IsProtected(branch)` checks the in-memory list (loaded
from `config.Load().PermanentBranches`) plus the well-known set. Returns on first match.
Guard 2 — `isProtectedRemote(branch, opts.ProtectedRemote)` reads the batched protected-branch
set when the caller fetched one, else falls back to the per-branch `IsProtectedOnGitHub` API call.

**Test reference.** `TestInvariant_I2_ProtectedBranchImmunity` in `prune_invariants_test.go`.

**Rationale.** Deleting `main` or `homolog` would be catastrophic. The double guard
(local + GitHub) ensures protection even when local config drift occurs.

---

## I3: Tombstone-Precedes-Deletion

**Statement.** `WriteTombstone(branch, sha)` MUST succeed before any local or remote
deletion call is made. If tombstone writing fails, no deletion occurs.

**Enforcement.** `prune.go`, the apply path in `PruneBranch` — it calls `WriteTombstone` and returns
early on error before calling any `git branch -d` or `git push origin --delete`.

**Test reference.** `TestInvariant_I3_TombstonePrecedesDeletion` in `prune_invariants_test.go`.

**Rationale.** Tombstones are refs under `refs/tombstones/<branch>` that preserve the branch
SHA for up to 7 days (GCTombstones TTL). Without the tombstone, a prematurely-deleted branch
cannot be recovered. The 4-iteration fix chain added this guard after `d9d47f02` found cases
where deletion happened but recovery was impossible.

---

## I4: Merge OR-Gate with SHA-Ancestor Verification

**Statement.** `IsMergedWithIndex(branch, base, ix)` returns true only when at least ONE of
these holds: (a) `git branch --merged base` lists the branch, OR (b) a merged GitHub PR exists
AND the PR's merge commit SHA is a confirmed ancestor of `base` via `git merge-base
--is-ancestor` AND the branch's live tip still equals the head commit GitHub recorded for that
PR (no commits pushed after the merge — see the stray-tip note below).

**Enforcement.** `prune.go` — `IsMergedWithIndex` tries the git path first (fast); on failure
or a false result, it resolves the merged PR either from the batched `PRIndex` (B-0348, one
`gh api --paginate` fetch per run) or, when no index is available, from the per-branch
`mergedPRViaGH` call. Both paths then run the SAME `isMergeCommitAncestor` +  `hasStrayTip`
verification — I4 does not depend on where the PR metadata came from. `IsMerged(branch, base)`
is the nil-index wrapper. Returns `MergeSourcePRVerified` only when both checks pass.

**Stray tips.** `hasStrayTip` compares `git rev-parse <branch>` against the PR's recorded
`head.sha`. It deliberately does NOT test "tip is an ancestor of base" — under squash-merge a
cleanly merged branch's tip is *never* an ancestor of base, so that test would hold back every
squash-merged branch and prune would delete nothing (`TestIsMergedWithIndex_SquashMergedBranchStillPrunes`
is the regression guard). It fails OPEN when either SHA is unknown.

**Test reference.** `TestInvariant_I4_MergeORGate` (git path),
`TestIsMergedWithIndex_PRMergedButShaNotAncestorRefuses`,
`TestIsMergedWithIndex_SquashMergedBranchStillPrunes`, and
`TestIsMergedWithIndex_CommitsPushedAfterMergeAreRefused` in `prindex_test.go`.

**Rationale.** GitHub's PR API may report a branch as "merged" before the merge commit is
actually reachable from `base` (race window after squash-merges). The SHA-ancestor check
eliminates this race. The 4-iteration fix chain discovered this during `6f4c720a`. The
head-SHA check was added by B-0348 after `afterpay296` — PR #334 merged, then more commits
landed on the branch; the merge-commit check alone would have deleted work living nowhere else.

---

## I5: Open-Plan Blocking

**Statement.** If any `.planning/*-{approved,reviewed,in-progress}.md` file contains
`branch: <name>` matching the candidate branch, `PruneBranch` skips it.

**Enforcement.** `prune.go`, Guard 4 in `PruneBranch` — `HasOpenPlanRef(branch)` scans `.planning/` for
files whose frontmatter `branch:` field matches. Returns the path of the first match.
`PruneBranch` returns early with `SkipReason = SkipOpenPlan`.

**Test reference.** `TestInvariant_I5_OpenPlanBlocking` in `prune_invariants_test.go`.

**Rationale.** An in-progress plan means active work. Deleting the underlying branch while
a plan is executing causes the worker to lose its ref target. The guard was added after
`99721c4e` when a parallel-session prune deleted a branch that another worker was still
pushing to.

---

## I6: Active-Command Blocking

**Statement.** If `/tmp/agent-audit-$CLAUDE_SESSION_ID/active-command` exists and is not
bypassed via `KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD=1`, `PruneBranch` skips ALL branches.

**Enforcement.** `prune.go`, Guard 5 in `PruneBranch` — `HasActiveCommand()` checks for the marker file.
Returns early with `SkipReason = SkipActiveCommand`.

**Test bypass.** Set `KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD=1` in tests (via `t.Setenv`).
This env var is read inside `HasActiveCommand()`.

**Test reference.** `TestInvariant_I6_ActiveCommandBlocking` in `prune_invariants_test.go`.

**Rationale.** Pruning while a skill is executing (audit-gated prune commands) risks
interfering with the skill's git operations. The bypass env var allows test suites running
inside a Claude Code session to proceed without the guard false-firing.

---

## I7: Local + Remote Deletion Independence

**Statement.** Failure to delete a remote ref does NOT abort the local deletion, and vice
versa. Each deletion attempt is independent; both results are recorded in `PruneDecision`.

**Enforcement.** `prune.go`, the delete steps in `PruneBranch` — it runs local deletion (`git branch -d`)
and remote deletion (`git push origin --delete`) in separate code paths. A non-nil error
from one path is recorded in `d.Error` but does not `return` before the other is attempted.

**Test reference.** `TestInvariant_I7_LocalRemoteIndependence` in `prune_invariants_test.go`.

**Rationale.** Remote deletion can fail transiently (network, permissions) while local
deletion succeeds. Keeping both attempts independent prevents a temporary remote failure
from leaving stale local branches. Discovered during `c2e27c78`.

---

## I8: Worktree is OFF-LIMITS (both modes, unconditional, fail-closed)

**Statement.** A branch checked out in any worktree is skipped in **both** dry-run and `--apply`
modes, for **every** candidate — local, remote-only, or misclassified refs alike. Prune never
removes a worktree and never deletes a worktree-backed branch — neither its local ref nor its
remote ref. The skip fires before the merge check, so a merged worktree-backed branch is still
left fully intact and reported as `SKIPPED-WORKTREE`. If the worktree listing itself cannot be
obtained, the check FAILS CLOSED: `runBranchPrune` aborts the whole run, and `PruneBranch` with
a nil set skips the branch rather than proceed to deletion blind.

**Enforcement.** `prune.go` Guard 6 — the branch → worktree-path set is parsed **once** by
`WorktreeBranches()` (`git worktree list --porcelain`, errors propagated) in `runBranchPrune`
(`cli/cmd/branch_prune.go`) and injected via `PruneOpts.WorktreeBranches`. `PruneBranch` consults
the set UNCONDITIONALLY — there is no `loc.HasLocal` gate (the old gate let a remote-only
misclassification bypass the guard). Nil set → `PruneBranch` lists worktrees itself and skips
fail-closed on error. Hit → `SkipWorktree` decision, no deletion of any ref, in both modes.

**Test reference.** `TestInvariant_I8_WorktreeOffLimits` (skip + worktree-survives +
branch-survives in dry-run, `--apply`, and `--apply` with the injected production set),
`TestInvariant_I8_RemoteOnlyClassificationStillSkips` (HasLocal=false candidate still skipped),
and `TestInvariant_I8_WorktreeListingErrorPropagates` (listing error is propagated, not
swallowed) in `prune_invariants_test.go`; `TestPruneBranch_Apply_SkipsAndPreservesWorktree` and
`TestPruneBranch_DryRun_DoesNotTouchWorktree` in `prune_test.go`.

**Rationale.** Worktree teardown is owned solely by `/worktree destroy` (the sanctioned
`kaisser worktree cleanup <path>` path), which does the proper teardown (Herd unlink + TLS cert +
Redis-prefix flush) that prune cannot. A force-removing prune left Herd links, certs, and Redis
keys dangling and nuked an in-use worktree after `/finish`. The old guard also failed OPEN on a
git error and was gated on `loc.HasLocal`, so a listing hiccup or a remote-only misclassification
could delete a live worktree's branch — both holes are closed (P-0185). The remote ref is pruned
automatically on a LATER pass: once `/worktree destroy` removes the worktree + local branch, the
branch no longer appears in the worktree set and Guard 6 no longer applies. Changed from the old
stash-and-remove behavior (`95f1ecda`) after the force-removal regression.

---

## I9: PR-State Deletion Requires Positive Evidence

**Statement.** Among branches the merge OR-gate refused, exactly one class reaches the
deletion path: `rejected` — the branch has **at least one** pull request and **every** one of
them is `CLOSED` (none `OPEN`, none `MERGED`). `in-flight`, `stray-tip`, `no-pr`, and
`unknown` are always kept. Deleting a `rejected` branch additionally requires an **available**
PR index; when the batch fetch failed, nothing is ever classified `rejected`.

**Enforcement.** `prune.go` — `classifyAndMaybeReject` runs only after the merge check
refuses, and only after guards 1–6 have already excluded protected / HEAD / open-plan /
active-command / worktree-held branches. It calls `ClassifyPRs(opts.PRIndex.For(branch))`;
`opts.PRIndex.Available()` gates the call, so an unavailable index yields `PRClassUnknown` and
the pre-B-0348 flat `SkipUnmerged`. Only `PRClassRejected` returns `true` (continue), and only
when `opts.ExcludeRejected` is false. Rejected deletions take the SAME tombstone-first path as
merged deletions (I3 applies unchanged).

**Default-on.** Plain `--apply` deletes `rejected` branches with no extra flag and no extra
prompt — the operator's standing decision (2026-08-09). `--exclude-rejected` is the opt-out;
there is deliberately no `--include-rejected`.

**Test reference.** `TestInvariant_I9_OnlyAllClosedPRsCanDelete` (all five classes) and
`TestInvariant_I9_ExcludeRejectedHoldsEverything` in `prune_invariants_test.go`;
`TestPruneBranch_Rejected*`, `TestPruneBranch_PRStateSkipClasses`,
`TestPruneBranch_UnavailableIndexNeverRejects`, and
`TestPruneBranch_RejectedStillYieldsToEarlierGuards` in `prindex_test.go`.

**Rationale.** Ancestry-only merge truth can never delete a branch whose PR was reviewed and
deliberately closed, so autofix-fleet branches pile up (31 of one repo's 45 "unmerged" skips
were rejected-PR branches). Pruning them is safe *because* the evidence is positive — a closed
PR is a human decision. The three keep-classes are exactly the cases where the evidence says
something else or says nothing: an open PR is live work, a stray tip holds commits that exist
nowhere else, and `no-pr` may be unpushed local work. `unknown` is the fail-safe: before
B-0348 a transient `gh` failure read as "no PR", and a batch fetch that half-succeeded would
read the same way, so `ParsePRIndex` marks any incomplete parse UNAVAILABLE rather than
partial.
