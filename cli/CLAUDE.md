# Claude CLI — Go Project Instructions

> 📊 **Knowledge graph available** — query the graph via `graphify query "<question>"` (CLI, from the repo root) before broad searches in this folder. See [`../CLAUDE.md`](../CLAUDE.md#graphify-knowledge-graph--query-this-before-grepping).

## Overview

`bravros` is a Go CLI binary that powers the Bravros SDLC. It provides audit enforcement, git context, PR helpers, Home Assistant integration, and more.

- **Module:** `github.com/bravros/bravros/cli`
- **Go version:** See `go.mod`
- **Framework:** [cobra](https://github.com/spf13/cobra) for commands

## Project Structure

```
cli/
├── main.go              # Entry point
├── cmd/                 # Cobra commands — the kernel (~29 files, 27 root verbs)
├── internal/
│   ├── backup/          # Backup utilities
│   ├── git/             # Git context helpers (branch, diff, PR info)
│   ├── github/          # GitHub API helpers (PR, repo)
│   ├── ha/              # Home Assistant API client
│   ├── plan/            # Plan/backlog id + entity resolution
│   └── worktree/        # Worktree lifecycle helpers (setup, cleanup, status)
├── testdata/            # Test fixtures
├── go.mod / go.sum
└── README.md
```

`internal/audit/` (the pre-P-0187 rule engine + `cmd/audit.go`, `cmd/rule.go`) was **deleted
outright** in P-0187 — no reduced rule count, no successor package. See
[`../docs/AUDIT-RULES.md`](../docs/AUDIT-RULES.md) for the guard-relocation table.

## StackConfig & lockfile heuristic

`internal/config.StackConfig` holds detected per-project stack info written into `.bravros.yml`. It now includes `NodePackageManager` (npm / pnpm / bun / yarn) populated by `stack.DetectNodePackageManager`.

Lockfile priority (bravros ships the full set; bravros is missing pnpm/bun/yarn — sync back on next DRY pass):

| Lockfile | Package manager |
|---|---|
| `bun.lockb` or `bun.lock` | `bun` |
| `pnpm-lock.yaml` | `pnpm` |
| `yarn.lock` | `yarn` |
| `package-lock.json` | `npm` |
| *(none, but `package.json` present)* | `npm` (default) |

`internal/worktree.SetupFull` (the `bravros worktree setup-full` verb) dispatches installs off `StackConfig.NodePackageManager`. When you add a new Node PM, update `DetectNodePackageManager` + the staleness lockfile slice in `internal/stack.checkStaleness` + this table.

## Development Commands

Standard Go tooling from `cli/` (`go build ./...`, `go test ./...`, `go test -run TestName ./internal/audit/`, `go vet ./...`). The only non-obvious invocation is the version-stamped local build:

```bash
cd cli && go build -ldflags="-s -w -X github.com/bravros/bravros/cli/cmd.Version=v1.9.5" -o ../bin/bravros .
```

## Local Build Rules

⛔ **Never install a locally-built binary. Install the way every user does.** `~/.claude/bin/bravros`
is owned by the release chain — it comes from `bravros update` / `selfupdate` / `install.sh`, which
download the signed tarball from the mirror Release and verify it against the minisign-signed
`checksums.txt`. A hand-built binary skips that verification and, worse, makes `bravros version`
lie: it reports a version string that matches no published release, so nothing downstream can tell
what is actually installed. That confusion cost a full debugging detour on 2026-08-19.

To ship a change, merge it and let the chain publish (§ Release & Tagging), then `bravros update`.
The round trip is ~3 minutes — cheaper than reasoning about a divergent local binary.

- **Build only to verify compilation**, never to install: `go build -o /tmp/bravros-check ./cli`
  and delete it. Never `-o ~/.claude/bin/bravros`, never `-o bin/bravros`.
- **`go test ./...` is the local feedback loop**, not a locally-installed binary.
- **`goreleaser` locally produces `dist/` (~72 MB, gitignored).** It is scratch output; the real
  artifacts are built by the mirror's `release.yml`. Don't install from it.
- **If a local binary must exist** for a one-off experiment, name it `bravros-dev` and keep it out
  of `~/.claude/bin/`. Always use `-ldflags "-X github.com/bravros/bravros/cli/cmd.Version=vX.Y.Z"`
  so it cannot masquerade as an unversioned build.
- **Never commit `bin/bravros`** — committed binaries break selfupdate's drift detection.

## Deploy (source repo → `~/.claude/`)

Two paths after editing skills/configs in this repo:

| Command | When |
|---|---|
| `bravros deploy` | Fast. Copies the source repo — cwd by default, or `--source <dir>` — to `~/.claude/` without running health checks. Use during active work. Supports `--filter <csv>` to override the `skills.enabled` allowlist per-invocation; `--dry-run` to preview. |
| `bash install.sh` | Full install. Runs dependency checks, 1Password auth, MCP platform filtering (removes Herd/BrowserMCP on Linux), and macOS-only setup. Use for first install or to update the binary itself. |

**`bravros selfupdate`** (alias: `bravros update`) — resolves the newest published release and downloads + signature-verifies `bravros-payload.tar.gz` when the on-disk payload is behind. Deploys `skills/` + `templates/` into `~/.claude/`. It never runs `install.sh`, never runs `git fetch`, and never touches a local clone. It stays a silent no-op when in sync or offline. Fires on SessionStart hook, rate-limited by the 6h check TTL (`BRAVROS_SELFUPDATE_TTL`).

**Drift detection** is now **commit-msg hook drift only** (`detectHookDrift`), which compares the project's `.githooks/commit-msg` and `.git/hooks/commit-msg` against the payload-deployed canonical at `~/.claude/templates/.githooks/commit-msg`. Payload freshness is decided by comparing the recorded payload tag against the latest published release tag, not by any git operation.

### Deploy manifest

**Path:** `~/.claude/skills/.deploy-manifest.json` (runtime-only, NEVER committed to source repo)

**Schema:** `{version: 1, skills: {<skillname>: <sha256>, ...}}`. The `skills` map records a deterministic SHA256 hash for each deployed skill's file content (using symlink-resolving walks). On every deploy, the CLI computes the current SHA for each enabled skill and compares it against the manifest; if the hashes match and `--force` is not set, the skill is skipped. If SHA differs, the skill is new, or the destination is incomplete, the skill is redeployed atomically (wipe + recopy). The manifest is updated after every real deploy (not dry-run). Missing manifest forces a full re-deploy on next invocation.

## Release & Tagging

**Versioning is AUTOMATED, and it does not happen in this repo.** Releases are cut from the
public mirror `bravros/bravros`. A merge to `main` here triggers
`.github/workflows/publish-public.yml`, which syncs the allowlisted subset to the mirror **and
computes the semver bump at publish time**, declaring it in the mirror commit subject
(`🚀 deploy: vX.Y.Z`). The mirror's own `auto-release.yml` *consumes* that declared version
rather than recomputing it, then its `release.yml` cross-compiles the three binaries
(`darwin-arm64`, `darwin-amd64`, `linux-amd64`) and publishes the GitHub Release. Version is
injected via `-ldflags="-X github.com/bravros/bravros/cli/cmd.Version=${VERSION}"`, so the
`Version` var in `cmd/root.go` is only a local-dev fallback.

**`publish-public.yml` is the ONE place a version is computed** (decision D6, P-0018). That is a
hard invariant: a second computation anywhere produces two counters over the same commits.

> **This repo carries no release tags.** `git tag` / `git describe` here is **not** the product
> version and must never be read as one. A `.github/workflows/auto-release.yml` used to live in
> this repo and tagged `main` independently; by 2026-08-19 it had drifted to a `v2.7.x` line while
> the shipped product was `v2.14.1`, and `git describe --tags` returned `v2.7.1`. It was removed.
> **Do not reintroduce a tagging workflow here.** Ask the mirror instead:
>
> ```bash
> gh release list --repo bravros/bravros --limit 5     # what actually shipped
> gh release view --repo bravros/bravros --json tagName -q .tagName
> ```

### Default flow: PR → homolog → main → publish → release

1. Feature branch → PR → merge to `homolog` (via `/finish`).
2. Open a homolog → main PR (also `/finish` step 7, or `/hotfix` for the emergency path) and merge it.
3. **`publish-public.yml` fires** on the main merge. It syncs the allowlisted subset to the
   mirror, computes the bump from the private commit subjects since the last sync, and pushes a
   mirror commit whose subject declares the version.
4. **The mirror's `auto-release.yml` fires**, reads the declared version, and tags it.
5. **The mirror's `release.yml` fires** on that tag, builds the binaries, publishes the Release.
6. Wall-clock: ~3 minutes from main merge to a downloadable release (observed 2026-08-19 —
   PR #63 merged 06:10:26Z, `v2.14.1` published 06:13:16Z).
7. Users pick it up on the next `bravros selfupdate`, which auto-fires from the SessionStart hook.

You run no `git tag` / `git push origin v*` commands at any point.

### Semver rules

The bump is computed in `publish-public.yml` from the private commit subjects replayed since the
last sync. Emoji prefixes (`🐛 fix:`, `✨ feat:`) are stripped before matching, and the accepted
type set is read from `templates/commit-types.txt` — the single source of truth.

| Commit pattern | Bump |
|---|---|
| `BREAKING CHANGE` in body, or `<type>!:` prefix | major |
| `feat:` / `feat(scope):` | minor |
| any other canonical type (`fix:`, `hotfix:`, `refactor:`, `perf:`, …) | patch |
| no typed commit found | **patch** — see below |

The highest bump in the range wins: one `feat:` among ten `fix:`es still yields a minor bump.

**There is no skip path, and `[skip-release]` does nothing here.** Unlike the removed
`auto-release.yml`, `publish-public.yml` never skips — a mirror content change always needs a
version to declare, so an untyped range still gets a patch bump. If a change must not ship, keep
it out of the publish allowlist.

> **Don't promise a version number in a plan.** The bump is decided at merge time from the actual
> commits, not from what the plan predicted. P-0129 documented v3.27.0 and shipped v3.26.1 because
> every commit was `fix:`-prefixed. Promise "the next semver bump publish computes."

### Verify a release

```bash
gh release list --repo bravros/bravros --limit 5
gh release view --repo bravros/bravros vX.Y.Z --json tagName,assets -q '.assets[].name'
gh run list --repo bravros/bravros --workflow=release.yml --limit=1
```

To confirm a specific fix actually shipped, read the file at the tag rather than trusting the
version string:

```bash
curl -sL https://raw.githubusercontent.com/bravros/bravros/vX.Y.Z/cli/<path> | grep <symbol>
```

### Manual tagging — mirror-only, and rarely

Manual tagging is a **mirror** operation (`bravros/bravros`); tagging this repo achieves nothing.
It is the override path for: forcing a higher bump than the commits imply, recovering from a tag
on the wrong commit, or re-tagging a release the mirror skipped. **Never move an existing tag** —
published Releases and binaries are immutable from a user's perspective, and editing them
invalidates `bravros selfupdate`'s drift detector. Bump the patch and tag again instead.

### Common gotchas

- **Never build binaries locally for a release.** Users get binaries from the mirror's Release
  assets via `install.sh`.
- **Don't commit `bin/bravros`.** It is a local dev build only.
- **A local binary can outrank the published one.** `bravros version` reports what is installed,
  which after a local `go build` + deploy may be ahead of, behind, or divergent from the mirror's
  latest. Check the mirror before concluding anything about "the" version.
- **Don't manually create a GitHub Release.** The mirror's Action builds, creates, attaches and
  generates notes on its own.

### Rule: Don't reference unreleased CLI flags

Never reference a `bravros` CLI flag in `settings.json`, hook commands, or skill files until the
release containing that flag has published on the mirror. `install.sh` has no local-binary path —
it always downloads `bravros-${OS}-${ARCH}.tar.gz` from the mirror Release (`install.sh:321`), so
**every** user hits a flag-not-found error on every hook fire until the release ships and they
`selfupdate`.

Safe order:
1. Add the flag to `cli/cmd/<x>.go`, merge to main, let publish ship it.
2. Wait for `gh release view --repo bravros/bravros` to show the new release.
3. THEN reference the flag in `settings.json` / hooks / skills, in a separate commit.

## Audit Rules — deleted (P-0187)

`cli/internal/audit/` (the ~50-rule `RuleDescriptor` registry, the PreToolUse hook, `cmd/audit.go`,
`cmd/rule.go`) was deleted outright — there is no rule engine left to author against. Guards for
what those rules used to catch now live inside the destructive primitive itself
(`discard`/`clean-untracked`/`trash`, `promote`/`destructive`/`pr-review unlock`), in git hooks
(`pre-push`, `commit-msg`), or in the platform's own `permissions` allow/deny lists. See
[`../docs/AUDIT-RULES.md`](../docs/AUDIT-RULES.md) for the full guard-relocation table. Do not
add new code under `cli/internal/audit/` — the directory does not exist.

## Backlog ID Handling — Authoring Notes (surfaced 2026-04-24)

Preserve this pattern when modifying `cli/internal/plan/backlog.go`:

1. **`backlogIDRe` must accept both bare-digit and B-prefixed filenames.** The canonical regex is `^(?:B-)?(\d{3,4})` — do NOT narrow it back to `^(\d{3,4})`. `normalizeBacklogID` strips `B-` before `Atoi` and zero-pads to 4 digits.

> **Note (B-0141, 2026-04-27 / superseded by P-0116, 2026-05-05):** The `gcStalePlaceholders` function and the entire `*-.placeholder` reservation mechanism were removed in P-0092 because consumer skills ignored the placeholders and called `bravros nextid` again, producing orphans. **Reinstated in P-0116** with mandatory consumer-side reuse: `ReservePlaceholder`/`ReleasePlaceholder` in `meta.go` and a new `bravros nextid reserve <entity>` verb. Consumer skills now MUST rename the placeholder into the final filename rather than calling `nextid` a second time. The new placeholder filename is `<id>.placeholder` (no trailing hyphen — `numberedFileRe` extended to match). No automatic TTL/GC: `bravros nextid release <ID>` is the explicit cleanup verb.

> **Note (P-0149, 2026-05-16):** Entity definitions are now centralized in a canonical `EntityDef` registry — `cli/internal/plan/entity.go` (`AllEntities`, `EntityByName`, `EntityByPrefix`, `AllPrefixes`). `nextid reserve/release`, `graph.go`'s prefix list, and the deprecated all-entities `nextid` JSON command all derive `{dir, prefix}` from the registry instead of hardcoded maps — add a new entity in one place. Each `EntityDef` carries a `kind`: `file` (plan/backlog/report/user_report — the placeholder-rename flow above applies) or `directory` (the `debug` entity, prefix `D-`, dir `.planning/debug`). For `kind: directory`, `bravros nextid reserve debug --slug <slug>` creates the `S-NNNN-<slug>-open/` directory **directly** — no `.placeholder` file, no consumer-side rename. Stage transitions (`-open/` → `-complete/`) run inside the CLI via `advanceEntity` / `bravros finish` (never a raw `mv` — keeps audit Rule 16 satisfied); `findEntityFileByID` resolves a bare `S-NNNN` to its directory regardless of stage suffix.

> **Note (P-0172, 2026-05-27):** Phase 4 of P-0170 is complete. `bravros meta` now calls `plan.ResolveWriteRoot()` instead of the (now-deleted) `plan.ResolvePlanningRoot()`, so `bravros meta --field plan_file` from inside a linked worktree returns the **calling worktree's** path rather than the primary clone's. The B-0208 redirect that this reverses was a since-superseded fix — see `.planning/debug/D-0004-bravros-meta-plan-file-worktree-path-open/report.md` for the runtime evidence that motivated the reversal.

> **Note (P-0180, 2026-07-02):** The `plan` entity is now dual-kind (`cli/internal/plan/entity.go` — `EntityDef.AllowsDirectory` / `IsDualKind()`): a plan is either a single `.planning/NNNN-slug.md` file (unchanged, `Kind` stays `EntityKindFile`) OR a `.planning/P-NNNN-<slug>/` folder whose canonical entry file is `PLAN.md` (fallback order: `PLAN.md` → id-prefixed `*.md` → `TASKLIST.md` → first frontmatter-bearing `.md`, resolved via `plan.ResolvePlanEntryFile` in `resolve.go`). Every file-only plan consumer (`FindPlanFile`, `ParsePlanHeader`, `CheckPlanCheckStatus`, the lint filename check, `scanWorktreeFS`/`scanBranchTree`) routes through this one resolver instead of ad-hoc per-call-site folder logic. `bravros nextid reserve plan --slug <slug>` creates the folder-plan (`.planning/P-NNNN-<slug>/` + seeded `PLAN.md`) via `plan.ReservePlanDir` (mirrors `ReserveScoutDir`); `bravros nextid reserve plan` **without** `--slug` is unchanged — it reserves a single-file `<id>.placeholder` (so `/plan`, which reserves with no slug, keeps its file-based flow). **Folder-plan id resolution:** `FindPlanFile` hands consumers the resolved entry file (`…/PLAN.md`), whose basename carries no id, so `ParsePlanHeader`/`CheckPlanCheckStatus` recover the plan number from the PARENT directory basename via the shared `planNumFromPath` helper (`meta.go`). `bravros finish` / `bravros plan advance` renames a folder-plan `P-NNNN-<slug>/` → `P-NNNN-<slug>-complete/` via `AdvancePlanDir` (mirrors `AdvanceDebugDir`); single-file plans keep the `-complete.md` rename unchanged. Audit rules 6/20 exempt anything at `.planning/<subdir>/**` (depth ≥ 2) from the plan-template gate — see `cli/internal/audit/CLAUDE.md` § "Plan-folders and the depth exemption".

## Key Conventions

- Import paths must use full module: `github.com/bravros/bravros/cli/internal/...`
- Autonomous pipeline skills: `auto-pr` (`--worktree` flag replaces the old `auto-pr-wt` alias); `auto-merge`/`merge-chain` retired — `batch-merge-prs` is the merge survivor (P-0187 decision 2)
- No progress-echo checkpoints — retired repo-wide 2026-08-04 (B-0347)
- The `bin/` directory in the repo root contains local copies for `install.sh` — these are separate from release assets

## Docs-sync requirement

When you add, rename, or change a CLI verb / flag / output shape under `cli/cmd/` or `cli/internal/`, you MUST update [`../docs/CLI.md`](../docs/CLI.md) — the grouped verb index — in the same PR: add/rename the verb's one-liner, note new load-bearing flags there, delete the row when a verb is removed.

The richer targets are now ported (P-0015): [`../example-bravros-cli.md`](../example-bravros-cli.md) — the skill-author CLI contract, treat it like a public API — and the `docs/cli/<group>.md` deep-dive tree, currently [`../docs/cli/install-update.md`](../docs/cli/install-update.md). **All three must be updated in the same PR.** `example-bravros-cli.md` documents a subset of verbs in full and lists the rest with their `Short:` line; expand a section when you touch its verb. This effectively closes [[B-0014-restore-docs-sync-rule]]. `bravros <verb> --help` remains the authoritative per-verb reference — it is generated from `cli/cmd/*.go` and cannot drift.

`bravros audit-docs` (the CI drift-linter for `docs/cli/*.md` flag tables) was retired with the
audit engine in P-0187 — there is no automated docs-code sync check anymore. Verify manually
against `cli/cmd/*.go` `Use:`/flag definitions before merging a CLI-surface PR.
