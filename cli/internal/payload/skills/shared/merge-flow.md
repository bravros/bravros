# Shared Merge Recipe

Canonical PR-merge flow used by `/finish`, `/promote`, `/hotfix`, and `/auto-pr
--worktree`. Skills LINK here instead of inlining merge prose, so the lock + merge call
stay uniform everywhere.

## When to Use the Lock vs Skip It

`bravros merge-lock {acquire,release,status}` serializes merges across concurrent
worktrees and orchestrators. If more than one Claude session could plausibly be merging
in this repo at the same time, acquire the lock. The only intentional exception is
`/hotfix` — one emergency at a time; lock-wait latency is not acceptable in an incident.
A lock-skipping skill documents the skip inline in its own SKILL.md — never silently
omits the calls.

## Recipe (Canonical Sequence)

The ordering is a safety property: lock before merge, verify before reporting success,
release on every exit path.

> **Runtime note.** `skills/shared/` was historically source-only — `bravros deploy` skips it
> (`NonRuntimeSkillDir`), so a deployed skill's `../shared/merge-flow.md` link resolved to nothing
> and the gates here were silently absent at merge time. If a Read of this path fails, you are on a
> runtime that predates the fix: fall back to the calling skill's own `references/` and report the
> dangling link instead of merging ungated.

### 1. Acquire the lock

Step 0: `pwd && echo "$(git branch --show-current)"` — confirm the expected branch before
taking the lock.

```bash
bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=<skill-name> --meta pr=<PR>
```

- On contention the command waits up to `--timeout` then exits non-zero. Surface the
  holder's `--meta` (from `bravros merge-lock status --json`) and **stop** — do not merge.
- The lock auto-extends every ~8 minutes while the parent process is alive; on hard-kill
  the OS file lock auto-releases.

### 1b. Readiness gate (mandatory — CI green is a different fact)

```bash
gh pr view <PR> --json mergeable,mergeStateStatus -q '"mergeable=\(.mergeable) status=\(.mergeStateStatus)"'
```

**Merge only at `mergeStateStatus: CLEAN`.** The calling skill's CI wait proves the checks it knew
about went green; this proves GitHub considers the PR mergeable *now*. The two diverge whenever a
check was added after the wait, a required review lapsed, or the base moved — and they diverge
almost always on a **freshly opened PR**, whose checks are all `queued` for the first seconds of
its life. `UNSTABLE` = a gate still running or a non-required check failed: re-enter the CI wait,
and if it persists, name the check and ask. `BLOCKED` = unsatisfied required gate. `DIRTY` =
conflicts → step 3. `BEHIND` = update the branch first. `UNKNOWN` = GitHub still computing
(~5 min, see R-0003 below) → one bounded wait, then decide on `mergeable` alone:

```bash
until [ "$(gh pr view <PR> --json mergeStateStatus -q .mergeStateStatus)" != "UNKNOWN" ]; do sleep 5; done
```

Never `sleep N; <check>` — the Bash tool blocks bare foreground sleeps; always the `until` form.
`mergeable=MERGEABLE` alone is **not** the gate: PR #1922 was `MERGEABLE`+`UNSTABLE` and got
merged with its main-branch protection check still pending.

### 2. Merge the PR

```bash
gh pr merge <PR> --merge
```

- `--merge` (merge commit) by default. **Never `--auto`** (see below). `--squash`/
  `--rebase` only when the calling skill explicitly documents the override.
- Clean merge → exit 0 → step 4. Conflict → non-zero → step 3.

### 3. Conflict path

**Never default to `--theirs`** — conflicts encode real semantic divergence that needs
in-session judgment. Recipe-side responsibility:

```bash
gh pr view <PR> --json mergeStateStatus,files,mergeable -q '{state: .mergeStateStatus, files: [.files[].path]}' > .merge-conflict.json
```

Surface the conflicted file list, **release the lock (step 5) before exiting** so other
skills are not blocked, and hand control back. The calling skill resolves per-file with
full source context, commits, pushes, and re-enters at step 1. The recipe never edits files.

Narrow `--theirs` exceptions (regenerated lockfiles, machine-generated migration
timestamps) live in the calling skill's SKILL.md, scoped to specific paths — never here.

### 4. Verify the merge landed

```bash
STATE=$(gh pr view <PR> --json state -q .state)
[ "$STATE" = "MERGED" ] || { echo "⚠️ unexpected post-merge state: $STATE"; exit 1; }
```

Anything other than `MERGED` → surface and stop. **Do not retry** — retry loops are what
caused R-0003.

### 5. Release the lock

```bash
bravros merge-lock release
```

Idempotent — safe even if step 1 failed or the kernel already auto-released.

## Why No `gh pr merge --auto`

Hard-won (R-0003): GitHub returns `mergeStateStatus: "UNKNOWN"` for ~5 minutes after a PR
opens while it computes mergeability. The old `bravros merge-pr` poll interpreted UNKNOWN
as "not yet ready" and held sessions captive ~5 minutes at a time. The recipe therefore
uses **one** synchronous `gh pr merge --merge` call — merge, definitive conflict, or
definitive auth error. No polling, no retry, no hang. CI gating is the calling skill's
responsibility **before** entering this recipe.

## `bravros merge-lock` Flag Surface

```
bravros merge-lock acquire [--timeout 60s] [--ttl 10m] [--meta key=value]...
bravros merge-lock release
bravros merge-lock status [--json]
```

- `--timeout` — wait for a contended lock (default `60s`). `--ttl` — validity without
  refresh (default `10m`; auto-refreshed ~8 min while the holder lives).
- `--meta key=value` — repeatable; surfaced by `status` (typical: `reason=`, `pr=`, `session=`).
- Stale locks: TTL + 30s grace elapsed AND holder PID dead → next `acquire` clears it.

`status --json` returns:

```json
{"held": true, "since": "2026-05-14T12:34:56Z", "ttl_remaining": "8m22s", "meta": {"reason": "finish", "pr": "128"}}
```
