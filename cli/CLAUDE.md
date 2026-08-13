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

- **Always use `-ldflags` with version** when building for `bin/` or `~/.claude/bin/` — never `go build -o` without it
- **Version tag format**: `-X github.com/bravros/bravros/cli/cmd.Version=v1.9.5` — the `v` prefix is stripped at print time, so always include it for consistency with git tags
- **Never commit `bin/bravros`** — releases ship via tag-push only (`release.yml` cross-compiles all 3 platform binaries) and manually committed binaries break selfupdate's drift detection. For in-session iteration, build a separate `bravros-dev` binary instead of overwriting the live one

## Deploy (portable repo → `~/.claude/`)

Two paths after editing skills/configs in this repo:

| Command | When |
|---|---|
| `bravros deploy` | Fast. Copies the portable repo to `~/.claude/` without running health checks. Use during active work. Supports `--filter <csv>` to override the `skills.enabled` allowlist per-invocation; `--dry-run` to preview. |
| `bash install.sh` | Full install. Runs dependency checks, 1Password auth, MCP platform filtering (removes Herd/BrowserMCP on Linux), and macOS-only setup. Use for first install or after a `selfupdate`. |

**`bravros selfupdate`** (alias: `bravros update`) — drift detector. If drift is detected it runs `install.sh` automatically; otherwise it's a silent no-op. Fires on SessionStart hook.

**Detector tiers (4m6).** The default run checks three *cheap* signals only — git HEAD drift (`HasOriginMainUpdates` via `merge-base`), CLI version drift (`detectCliStale` via `git describe`), and commit-msg hook SHA drift (`detectHookDrift`). The two *expensive* signals — per-skill manifest-SHA drift (each enabled skill's tree is walked + hashed) and `scripts/` drift — run only under `--deep`. They are redundant on the common path: any skill or script edit pushed to main lands as a new `origin/main` commit, so the git-HEAD signal already fires and triggers `install.sh`. `--deep` covers the rare case where the deployed runtime drifted WITHOUT a new main commit (manual `~/.claude` edit, partial deploy, manifest loss).

**Clobber guard (WS6).** The working-tree overlay `git checkout origin/main -- .` runs ONLY when HEAD is strictly an ancestor of `origin/main` (clean catch-up / fast-forward — `selfupdate.IsBehindOriginMain`). On a diverged or ahead HEAD (e.g. homolog or a feature branch carrying committed-but-not-yet-on-main work), the overlay is skipped so local commits' content is never reverted into a staged reversion. `install.sh` still runs on a diverged HEAD for any skill/CLI/hook drift — only the destructive overlay is gated.

Skill-drift signal is manifest-SHA-based (P-0138): under `--deep`, any change inside a skill (file added, removed, content modified, regardless of which file or mtime) triggers redeploy on next SessionStart. The manifest lives at `~/.claude/skills/.deploy-manifest.json` and is written by `install.sh` via `bravros deploy`. Missing manifest → forces a fresh `install.sh` run.

### Deploy manifest

**Path:** `~/.claude/skills/.deploy-manifest.json` (runtime-only, NEVER committed to source repo)

**Schema:** `{version: 1, skills: {<skillname>: <sha256>, ...}}`. The `skills` map records a deterministic SHA256 hash for each deployed skill's file content (using symlink-resolving walks). On every deploy, the CLI computes the current SHA for each enabled skill and compares it against the manifest; if the hashes match and `--force` is not set, the skill is skipped. If SHA differs, the skill is new, or the destination is incomplete, the skill is redeployed atomically (wipe + recopy). The manifest is updated after every real deploy (not dry-run). Missing manifest forces a full re-deploy on next invocation.

## Release & Tagging

**Tagging is AUTOMATED.** `.github/workflows/auto-release.yml` watches pushes to `main` that touch `cli/**`, computes the semver bump from conventional-commit subjects since the last tag, and pushes the tag itself. `release.yml` then cross-compiles all 3 binaries (`darwin-arm64`, `darwin-amd64`, `linux-amd64`) and publishes the GitHub Release. Version is injected via `-ldflags="-X github.com/bravros/bravros/cli/cmd.Version=${VERSION}"` from the tag name, so the source `Version` constant in `cmd/root.go` is just a local-dev fallback.

⛔ **NEVER push a tag manually** for a normal CLI ship — auto-release already does it. ⛔ **NEVER build binaries locally** for releases. Manual tagging is bypass-only — see "When to use manual tagging" below.

### Default flow: PR → homolog → main → auto-tag → release

This is the validated path (proven end-to-end during P-0129 / v3.26.1, 2026-05-07):

1. Feature branch → PR → merge to `homolog` (via `/finish`).
2. Open a homolog → main PR (also via `/finish` step 7) and merge it.
3. **`auto-release.yml` fires** within seconds of the main merge. It:
   - Computes the bump from commit subjects since the last tag (rules below).
   - Pushes the new tag to origin.
4. **`release.yml` fires** on the tag push. It cross-compiles 3 binaries and publishes the GitHub Release.
5. Total wall-clock time: ~1-2 minutes after the main merge completes.
6. Users pick up the new binary on next `bravros selfupdate` — which auto-fires via the SessionStart hook.

You don't run any `git tag` / `git push origin v*` commands during normal flow.

### Auto-release semver rules

`auto-release.yml` evaluates every commit subject between the last `vX.Y.Z` tag and the new main HEAD. Emoji prefixes (e.g. `🐛 fix:`, `✨ feat:`) are stripped before matching, so this repo's emoji+conventional style works the same as plain conventional commits.

| Commit pattern | Bump |
|---|---|
| `BREAKING CHANGE` in body, or `feat!:` / `fix!:` / `hotfix!:` prefix | major |
| `feat:` / `feat(scope):` | minor |
| `fix:` / `hotfix:` / `refactor:` / `perf:` / `chore:` / etc. | patch |
| No conventional-commit type found in the range | **skip** (no release) |

`hotfix:` is a patch-bump synonym for `fix:` — the `/hotfix` skill emits `🩹 hotfix: <description>`,
and a hotfix landing on main MUST tag a release. Added in v3.46.1 after a hotfix on v3.46.0 was
silently skipped (auto-release didn't recognize the prefix; manual tag-push was required to
recover). Both `auto-release.yml` and this table track the canonical accepted-prefix set.

The highest bump in the range wins. So one `feat:` mixed with ten `fix:`es still produces a minor bump.

> **Caveat noted on 2026-05-07:** plan/PR documents that pre-write a target version (e.g. "tag v3.27.0") may be wrong by one tier — auto-release decides the version at merge time from the commits, not from the plan. P-0129 documented v3.27.0 but actually shipped as v3.26.1 because every commit was `fix:`-prefixed. Don't promise a specific version in plan acceptance criteria; promise "the next semver bump auto-release computes."

### Skip a specific release

Add `[skip-release]` to the merged commit subject. `auto-release.yml` will exit without tagging. Use sparingly — for changes that touch `cli/` but don't need user-facing release (e.g., test-only edits).

### Dry-run before merge

To preview the version `auto-release.yml` would compute on the current main:

```bash
gh workflow run auto-release.yml -f dry_run=true
```

Then `gh run list --workflow=auto-release.yml --limit=1` to find the run, and view the logs for the computed tag.

### When to use manual tagging (bypass auto-release)

Auto-release covers ~all cases. Manual tag push is only needed when:

- **Forcing a higher bump than commits suggest.** If conventional-commit prefixes don't reflect actual semantics (e.g., a `fix:` that's actually a breaking API change), tag manually with the desired version BEFORE auto-release evaluates — push the tag, then push the main commit. Auto-release sees the new tag exists and the commit is at-or-before it, and skips.
- **Recovering from a wrong-commit tag.** See "Recovering from a wrong-commit tag" below.
- **Releasing on a non-main branch.** Auto-release only fires on `main`. Releases from `homolog` or feature branches require manual tag push.
- **Re-tagging a release that auto-release skipped.** If you intended a release but every commit was non-conventional and auto-release exited without tagging, push the tag manually.

### Manual release flow (override path)

Run from a `main` checkout. The pre-push hook (PR #132+) allows tag pushes from any branch including main, but blocks branch pushes to main.

```bash
# 1. Make sure local main matches origin/main
git checkout main
git pull origin main

# 2. Pick the version manually (auto-release would have computed it, but you're overriding)
NEW_VERSION="v3.27.0"  # check current: gh release view --json tagName -q .tagName

# 3. Tag and push
git tag -a "$NEW_VERSION" -m "🚀 deploy: $NEW_VERSION — <one-line summary>

<bullet list of what's in this release — usually copies from the merged PR's body>"

git push origin "$NEW_VERSION"

# 4. Watch release.yml (auto-release skips since the tag already exists)
sleep 8 && gh run list --workflow=release.yml --limit=1

# 5. Verify assets
gh release view "$NEW_VERSION"   # should show 3 binaries
```

That's it. Users running `bravros selfupdate` get the new binary on their next session.

### Common gotchas

- **Tag from main, not from a feature branch.** If the tag commit isn't on main, `git describe` from main won't find it and future tools (changelog, release-notes generation) get confused. The published binary still works either way — but lineage matters.
- **Don't tag from a pre-squash feat-branch commit.** Squash-merging a feature PR creates a brand-new commit on the base branch; the original feat-branch commits are orphaned once the branch is deleted. Tag the squash-merge commit instead.
- **Tag pushes ARE allowed from a `main` checkout.** The local pre-push hook only blocks branch pushes to main (`refs/heads/main`); it explicitly permits tag pushes (`refs/tags/*`). If you see "Direct push to main is not allowed!" while pushing a tag, your hook is out of date — pull main and re-deploy.
- **Don't manually create the GitHub Release.** Pushing the tag is enough — the Action builds binaries, creates the Release, attaches assets, and generates release notes automatically.
- **Don't commit `bin/bravros` for releases.** It's a local dev build only; users get binaries from the GitHub Release assets via `install.sh`.

### Recovering from a wrong-commit tag

If a tag was pushed against the wrong commit (e.g. a pre-squash feat commit), the cleanest fix is to bump the patch version and tag again from the correct commit. **Don't move an existing tag** — the published Release + binaries are immutable from the user's perspective and editing them invalidates `bravros selfupdate`'s drift detector.

```bash
# Wrong: v3.15.0 tagged at e63417b (orphan feat commit)
# Right: bump and re-tag
git checkout main
git pull origin main
git tag -a v3.15.1 -m "🚀 deploy: v3.15.1 — main-lineage tag of <whatever>"
git push origin v3.15.1
```

### Verify any release

```bash
gh run list --workflow=release.yml --limit=1   # build status
gh release view vX.Y.Z                          # assets + release notes
gh release view vX.Y.Z --json tagName,assets -q '.assets[].name'  # asset names only
```

### Rule: Don't reference unreleased CLI flags

Never reference a `bravros` CLI flag in `settings.json`, hook commands, or skill files until the release containing that flag is tagged AND the GitHub Action has published the binary. install.sh prefers a local `cli/bravros-${OS}-${ARCH}` binary if present, but most users get the binary from the GitHub release — those users will hit a flag-not-found error on every hook fire until they run `selfupdate`.

Safe order:
1. Add the flag to `cli/cmd/<x>.go`, commit + tag + push (Action ships the binary).
2. Wait for `gh release view` to show the new release.
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

When you add, rename, or change a CLI verb / flag / output shape under `cli/cmd/` or `cli/internal/`, you MUST update these files in the same PR (same rules as root `CLAUDE.md` docs-sync table):

| You changed | Also update |
|---|---|
| New verb or sub-command | [`../example-bravros-cli.md`](../example-bravros-cli.md) — single-source skill-author reference |
| Flag added/renamed | [`../example-bravros-cli.md`](../example-bravros-cli.md) (flag table for that verb) |
| Output shape changed | [`../example-bravros-cli.md`](../example-bravros-cli.md) (Sample output block) |
| Verb removed | Delete its section in [`../example-bravros-cli.md`](../example-bravros-cli.md) + remove from Quick Index |
| Any of the above | Plus [`../docs/CLI.md`](../docs/CLI.md) index + the right [`../docs/cli/<group>.md`](../docs/cli/) deep-dive |

`example-bravros-cli.md` is the canonical CLI surface for skill authors — if it's stale, skills break. Treat it like a public API contract.

`bravros audit-docs` (the CI drift-linter for `docs/cli/*.md` flag tables) was retired with the
audit engine in P-0187 — there is no automated docs-code sync check anymore. Verify manually
against `cli/cmd/*.go` `Use:`/flag definitions before merging a CLI-surface PR.
