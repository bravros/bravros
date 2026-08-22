# TIA storage, worktrees, and the graph

Read this when choosing storage, working inside a git worktree, or when someone proposes committing
the graph to the repo.

## Where the graph lives

By default: `~/.pest/tia/<project-key>/`, outside the repo — so there is nothing to `.gitignore`.
`vendor/bin/pest --baseline` always prints the **effective** path, configured or not. Use it rather
than reconstructing the key yourself; the key derivation is not stable across environments (see
below).

The directory holds `graph.json` (the test↔source dependency map, ~11 MB on an 18,000-test suite)
and optionally `coverage.bin.gz`.

## Do not set `directory()`

Three options get proposed. All three are worse than the default:

**Project-relative (`directory('.pest/tia')`)** — buys nothing. The docs are explicit that the
configured path is used verbatim, with no `<project-key>` segment appended, so two worktrees on the
same relative path keep *separate* caches — exactly what the default already does. Meanwhile it puts
an 11 MB file inside the repo that now needs a `.gitignore` entry, with a real chance of someone
committing it.

**A shared absolute path** — technically sound and still wrong. The graph stores project-*relative*
paths (that's why a graph recorded on a CI runner is adoptable on a laptop at a different absolute
path), so sharing genuinely works. But writes are atomic-rename, last-writer-wins, with no locking.
Point N worktrees on N branches at one directory and each full run overwrites the previous one's
graph; every other worktree then sees drift and re-records. That thrash is worse than the cold build
it was meant to avoid.

**Committing the graph** — the most tempting and the most expensive. It's ~11 MB, git keeps every
version, and the graph changes substantially whenever code does, so it won't delta-compress. A few
dozen refreshes and every clone carries the weight permanently; history can't be pruned without a
rewrite.

## Git worktrees: two things silently do not work

Both stem from the same root cause. In a linked worktree, `.git` is a **file** containing
`gitdir: …`, not a directory.

**1. `baselined()` silently no-ops.** `BaselineSync::detectGitHubRepo()` does
`is_file('<root>/.git/config')`. In a worktree that's false, so it finds no origin, returns null,
and skips the fetch **with no message at all** — indistinguishable from "no baseline published
yet". Changing the storage directory does not help: the fetch dies before storage is consulted.

**2. Worktrees never share a cache**, despite the docs saying they do. The project key comes from
`rawOriginUrl()`, which performs the same `is_file` check, so it falls back to hashing the absolute
path. A worktree's key looks like `myproject-branchname-7afd982784524a70`, not `org/repo`.

If most work happens in worktrees, factor that in: each one pays its own recording pass.

## Seeding a worktree instead of sharing

The graph is portable across checkouts, so a one-time copy warms a new worktree without any of the
clobbering problems of a shared directory:

```bash
cp -R "$(cd <main-clone> && vendor/bin/pest --baseline)/." \
      "$(cd <worktree>   && vendor/bin/pest --baseline)/"
```

`--baseline` prints the effective path on both sides, so the key hashing is never reimplemented. A
stale seed is harmless — Pest validates the graph against project state and re-records whatever
drifted.

This is a snapshot, not a live share: nothing is clobbered afterwards. Good candidate for whatever
tooling creates worktrees.

## What the graph can and cannot see

TIA builds its dependency map from **coverage**. That's the whole basis for trusting it, and the
whole limit:

- **Seen:** PHP source touched during execution — classes, controllers, models, helpers.
- **Not seen:** reflection, container bindings resolved by string name, config and settings reads,
  Blade templates, anything resolved at runtime in a way the coverage driver can't attribute.

This is why `--no-tia` remains the real gate for dependency bumps and framework upgrades: those are
precisely the changes most likely to move something the graph cannot model.

## Reading behaviour from source

The published docs and the installed version can disagree. On the reference migration the docs'
CI recipe was self-contradictory and only `vendor/pestphp/pest/src/Plugins/Tia/*` explained why.
When behaviour surprises you, grep the plugin source — it's readable, and the skip/abort paths all
emit distinctive messages you can search for.
