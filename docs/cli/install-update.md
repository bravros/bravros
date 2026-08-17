# CLI deep-dive — install & update

Covers `setup`, `selfupdate`, `update`, `deploy` and `install`. Index: [`../CLI.md`](../CLI.md).
User-facing narrative: [`../skill-delivery.md`](../skill-delivery.md).

Flags below are verified against `cli/cmd/setup.go`, `cli/cmd/selfupdate.go`,
`cli/cmd/update.go` and `cli/cmd/deploy.go`. `bravros audit-docs` — the CI drift-linter that used
to check these tables — was retired with the audit engine in P-0187, so this file is only correct
if you keep it correct by hand.

---

## `bravros setup`

```
bravros setup [flags]
```

Install bravros components into `~/.claude` from the payload **embedded in this binary** — no
network, no source checkout. Interactive (`charmbracelet/huh`) when stdin is a TTY; otherwise
driven by flags. `install.sh` / `install.ps1` `exec` into it as their last act.

`Args: cobra.NoArgs`. `bravros setup` (top level) and `bravros worktree setup` /
`bravros worktree setup-full` are different commands and do not shadow each other in dispatch,
help text, or shell completion.

### Flags

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--all` | bool | `false` | Install every component. **Implies `--skills=all`.** |
| `--components` | string | `""` | Comma-separated component ids. Overrides `BRAVROS_COMPONENTS`. |
| `--skills` | string | `""` (⇒ `core`) | Skill scope for `claude-skills`: `core` or `all`. Any other value is a hard error naming both valid values. |
| `--yes` | bool | `false` | Non-interactive: skip the picker and the confirm screen. |

Scope precedence: `--skills` → `--all` → the scope recorded by a previous run → the manifest
default (`core`).

### Environment

| Variable | Effect |
|---|---|
| `BRAVROS_COMPONENTS` | Component list honored when `--components` is absent. |
| `BRAVROS_INSTALL_METHOD` | Declares how the binary arrived; recorded in `state.json` and read later by `update`. Set by the installers. |
| `BRAVROS_ALLOW_PLUGIN_MANAGED` | Overrides the plugin-managed refusal. An env var rather than a flag on purpose — the safe path must be the short one. |
| `BRAVROS_CONFIG_DIR` | Overrides `~/.claude` as the install root (`config.ConfigDir()`). |

### Components

| id | Kind | Target (relative to `~/.claude`) | Default | Notes |
|---|---|---|---|---|
| `cli` | binary | `bin/` | on | Required; cannot be deselected. |
| `claude-skills` | embedded tree | `skills/` | on | Scoped: `core` (default) \| `all`. |
| `claude-templates` | embedded tree | `templates/` | on | Git hooks + project templates. |
| `claude-settings` | merged settings | `settings.json` | on | Deep-merged, never replaced. |

`core` resolves from `core: true` in each `SKILL.md` frontmatter inside the embedded FS — **18 of
35** today, asserted by test to equal `plugins/core/skills/` membership *by name*.

Targets are stored as path segments and joined with `filepath.Join`, so Windows resolves
`%USERPROFILE%\.claude\...` with no hand-written separator and no `%LOCALAPPDATA%` branch.

### Behavior contract

- **Never destructively overwrites.** An existing file that differs is left alone; the payload's
  version lands as `<name>.new` and is reported.
- **`settings.json` is deep-merged** entry by entry through `cli/internal/managed/`.
- **Pruning is scoped to `skills/` + `templates/`.** `hooks/`, `agents/` and `state/` are never
  pruned.
- **Plugin-managed installs are detected and refused**, never written into.
- **Idempotent.** A second identical run reports no changes.

### `state.json`

Path: `<config dir>/state/setup.json` — i.e. `~/.claude/state/setup.json`. `state/` is on
`deploy`'s never-prune allowlist, so neither `deploy` nor `selfupdate` can remove it.

| Field | Type | Meaning |
|---|---|---|
| `schema` | int | Record version. |
| `bravros_version` | string | The binary that wrote it. |
| `install_method` | string | `installer` \| `brew` \| `scoop` \| `source` \| `unknown`, or whatever `BRAVROS_INSTALL_METHOD` said. |
| `claude_root` | string | Absolute install root. |
| `skills_scope` | string | `core` \| `all` (omitted when empty). |
| `components[]` | array | Per component: `id`, `scope`, `skills[]` (the **resolved** names), `target`. |

The resolved skill list is stored rather than recomputed, so a later release that moves the
core/all split can still tell exactly what a machine has. There is deliberately **no timestamp**:
two identical runs must produce byte-identical state, which is what makes "the second run changed
nothing" observable rather than asserted. Invalid JSON is a hard error telling you to move the
file aside and re-run.

---

## `bravros selfupdate`

```
bravros selfupdate [flags]
```

Refresh `~/.claude` from the payload embedded in **this** binary. This is the automatic half of
the split update model (P-0015 D2, revised by P-0018 D2) and the command the SessionStart hook
runs. Its only network traffic is the rate-limited passive version check — but on a binary that
`install.sh` owns, that check is now a **trigger, not just a printer**: see the auto-update lane
below. On every other install (brew, scoop, source, unknown) it never replaces a binary and only
prints the familiar one-line notice.

### The SessionStart auto-update lane (P-0018 D2)

When the 24h check resolves a newer tag AND `setup.json` records `install_method: "installer"`
(the binary lives under `<claude root>/bin`, placed there by `install.sh`/`install.ps1`),
`selfupdate` downloads the release, verifies it against the minisign-signed `checksums.txt`
(the same trust chain as `bravros update` — one implementation, reused), atomically swaps the
executable, and re-runs `selfupdate --force` from the **new** binary so components refresh from
its embedded payload. The swap prints exactly one line: `🔄 bravros vX → vY (auto)`.

Guards, all shipping together (none optional):

- **Ownership refusal comes first.** A brew/scoop binary — by observed path *or* by
  setup.json's record — is refused before anything is downloaded. The automatic lane never
  lets observed reality overrule a stale brew record (unlike explicit `bravros update`).
- **Rollback:** the outgoing executable is kept beside the new one as `bravros.prev`
  (one generation; each swap overwrites it). Rollback is a `mv`, not a reinstall.
- **Young-release canary:** releases younger than ~6h are deferred to the next check, so a
  yanked release never reaches most of the fleet. `BRAVROS_MIN_RELEASE_AGE` overrides the
  window (Go duration; `0` disables). Age is derived from a release asset's `Last-Modified`
  header — no api.github.com budget spent. Unknown age → notify-only, never swap.
- **Opt-outs:** `BRAVROS_NO_UPDATE_CHECK=1` disables the whole lane;
  `"auto_update": false` in setup.json switches it back to notify-only (absent = on for
  installer-owned binaries).
- **Every unknown fails open to notify** — unreadable setup.json, unresolvable install
  method, undeterminable release age. Network failure stays silent and non-fatal, and the
  24h cadence is unchanged.

What it refreshes is whatever `state.json` records. An install predating that file (no
`setup.json`, but a populated `~/.claude/skills`) is refreshed at scope **`all`** — defaulting to
`core` there would delete every non-core skill the user was relying on.

### Flags

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--force` | bool | `false` | Bypass the check-TTL cache and refresh now. |
| `--dry-run` | bool | `false` | Show what would be updated; modify nothing on disk. |
| `--verbose` | bool | `false` | Print detailed trace output. |
| `--skip-if-recent` | string | `""` | Skip if the last update was within this duration (e.g. `6h`, `30m`). |
| `--fetch-payload` | bool | `false` | Legacy lane: fetch and deploy the published `bravros-payload.tar.gz` instead of using the embedded payload. |
| `--silent` | bool | `false` | **Deprecated no-op** — silence is now the default. |
| `--deep` | bool | `false` | **Deprecated no-op** — the clone-based drift detectors it gated no longer exist. |

### Environment

| Variable | Default | Effect |
|---|---|---|
| `BRAVROS_SELFUPDATE_TTL` | `6h` | Whole-run cache. A run inside the TTL returns immediately having done nothing. `0` disables. Marker: `~/.claude/state/.bravros-last-check`. |
| `BRAVROS_NO_UPDATE_CHECK` | unset | `1`/`true`/`yes` disables the passive check AND the auto-update lane entirely. |
| `BRAVROS_UPDATE_NOTICE_TTL` | `24h` | Minimum spacing between passive checks. `0` removes the rate limit. |
| `BRAVROS_MIN_RELEASE_AGE` | `6h` | Young-release canary window for the auto-update lane. `0` disables the canary; a garbage value falls back to the default. |
| `BRAVROS_REMOTE_CHECK_TTL` | — | Minimum spacing between remote release-tag lookups (`internal/selfupdate/remote.go`). |

Exit code proves nothing here: `selfupdate` returns `nil` on nearly every path, including "did
nothing". Verify by observing the filesystem.

---

## `bravros update`

```
bravros update [flags]
```

Download, verify and install the newest bravros binary. **This is its own verb, not a
`selfupdate` alias** — the alias was removed in P-0015 Phase 6, and a test asserts both that
`update` resolves to its own command and that `"update"` is absent from `selfupdateCmd.Aliases`
(leaving it would make cobra's resolution order-dependent and the bug silent).

Sequence: resolve the newest published release → download the archive for this platform → verify
it against the minisign-signed `checksums.txt` → atomically replace the running executable → run
the **new** binary's `selfupdate --force` so components are refreshed from its embedded payload.

`Args: cobra.NoArgs`. `SilenceUsage: true` — a refusal or a failed download is not a usage error,
and dumping the flag list would bury the one line naming what to run instead.

### Flags

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--check` | bool | `false` | Report whether a newer release exists; replace nothing. |
| `--force` | bool | `false` | Install even when the running version is already the newest. |
| `--tag` | string | `""` | Install a specific release tag instead of the newest. |

### Package-manager refusal

`update` refuses when the install is owned by a package manager, and names the right command
instead (e.g. `brew upgrade bravros`). Replacing a brew- or scoop-managed file leaves that
manager's metadata pointing at something it no longer controls, and its next upgrade silently
rolls the user back.

**Observed reality wins; `state.json` is a fallback only.** A binary sitting under `/Cellar/`,
`/homebrew/`, `/linuxbrew/` or `/scoop/` is refused even when the record says otherwise, and a
record saying `brew` when the binary demonstrably is not is not grounds to refuse. A missing
record is never by itself grounds to refuse.

### Platform note — the self-replace

POSIX: a single `rename(2)`, atomic, with the running process keeping its now-unlinked inode.
There is never an instant with no binary at the target path.

Windows: a running image is locked against replacement but may be renamed away, so the sequence
is rename-aside → install → delete-sideline, and that last step fails (a running image cannot
delete its own file). `bravros.exe.old-<rand>` is left behind and swept by `CleanupOldBinaries` on
a later run. The crash window between the two renames is real and is why native Windows is a
documented degraded tier (P-0015 D3). A failure at the second rename rolls the old executable
back into place.

---

## `bravros deploy`

```
bravros deploy [flags]
```

Deploy the toolkit runtime into the host config dir. Lower-level than `setup`: it copies from a
source tree on disk rather than from the embedded payload.

| Flag | Type | Effect |
|---|---|---|
| `--source` | string | Deploy from an explicit source dir instead of cwd — e.g. a payload fetched by `selfupdate` on a machine with no clone. Must contain `skills/` and `cli/go.mod`, or a subset like `skills/`+`templates/` for a fetched payload. |
| `--filter` | string | Comma-separated skill names; overrides `.bravros.yml:skills.enabled`. Core skills always deploy. |
| `--dry-run` | bool | List what would be deployed without copying. |
| `--count-only` | bool | Output only the count of files to deploy. |
| `--force` | bool | Overwrite every source file at the destination, skipping the mtime/hash skip-unchanged comparison; also downgrades the pre-deploy bash-hygiene lint to a warning. |
| `--no-prune` | bool | Preserve orphan skills/templates/hooks at the destination instead of removing them. |
| `--json` | bool | Emit the full `DeployResult` JSON object instead of the human summary. |
| `--field` | string | Extract a single field value (dot notation). |

---

## `bravros install`

```
bravros install
```

Host-oriented entry point that delegates to `install.sh`. Since the multi-harness adapters
(Codex/OpenCode/Pi) and the per-host skills compiler were retired in P-0187, Claude Code is the
only supported platform: global install is `bash install.sh`, project-scoped setup is
`bravros init`.

As of P-0018, `install.sh` and `install.ps1` are version-aware: they resolve the latest tag via
the no-API redirect on `/releases/latest` and compare it against any installed binary. Same
version → `already current (vX.Y.Z)` and no download; older → `updating vX.Y.Z → vA.B.C` before
downloading; `--force` (PowerShell: `-Force`, or `BRAVROS_FORCE=1` under `irm | iex`) reinstalls
regardless. Any failure in the version check falls back to the always-download behavior — the
installer never bricks on it.
