# `bravros` — canonical CLI surface for skill authors

This file is the contract between the Go kernel and every skill that shells out to it. Treat it
like a public API: if it is stale, skills break silently.

**Coverage.** Verbs marked ✅ below are documented in full here. The rest are listed in the Quick
Index with the `Short:` line from their own command definition and are not yet expanded — for
those, `bravros <verb> --help` is authoritative (it is generated from `cli/cmd/*.go` and cannot
drift). Expand a section here when you touch its verb.

**Maintenance rule** (`cli/CLAUDE.md` § Docs-sync requirement): a new verb, a renamed flag, or a
changed output shape must land in this file, in `docs/CLI.md`, and in the right
`docs/cli/<group>.md` deep-dive **in the same PR**. `bravros audit-docs`, the CI
drift-linter for these tables, was retired with the audit engine in P-0187 — verify by hand
against `cli/cmd/*.go` `Use:` and flag definitions.

> The `docs/` tree lives in the private development repo and is deliberately not published, so
> the `docs/…` paths named here are unlinked — they are directions for contributors working in
> that repo, not pages you can open from the public mirror. This file and `bravros <verb> --help`
> are the authoritative CLI surface for everyone else.

---

## Quick index

| Verb | Short | Documented here |
|---|---|---|
| `autopr` | Manage autonomous pipeline lock | — |
| `branch` | Branch management utilities | — |
| `clean-untracked` | Preserve into `.trash/` then remove untracked files (no token) | — |
| `commit` | Commit plan + code changes | — |
| `config` | Read project configuration values from `.bravros/config.json` | — |
| `deploy` | Deploy the toolkit runtime into the host config dir | `docs/cli/install-update.md` |
| `destructive` | Gate permanently destructive commands with a human-presence token | — |
| `discard` | Preserve into `.trash/` then discard uncommitted changes (no token) | — |
| `ha` | Home Assistant CLI | — |
| `hook` | Claude Code hook subcommands | — |
| `hooks` | Manage bravros-managed git hooks | — |
| `init` | Initialize the current repository with the SDLC structure | — |
| `install` | Install the toolkit runtime (delegates to `install.sh`) | `docs/cli/install-update.md` |
| `merge-lock` | Atomic merge-lock primitive (acquire / release / status) | — |
| `nextid` | Atomically reserve next plan, backlog, report and user-report IDs (JSON) | — |
| `pr-review` | Write the PR review stamp from the latest bot verdict (`--write-stamp`) | — |
| `promote` | Promote `homolog` → `main` with human-presence token | — |
| `secrets` | Manage bravros secrets (`op` / `env` / `none` backends) | — |
| `selfupdate` | Refresh installed components from this binary's embedded payload | ✅ |
| `setup` | Install bravros components from the embedded payload | ✅ |
| `statusline` | Render Claude Code status line (reads JSON from stdin) | — |
| `trash` | Inspect and manage the `.trash/` preserve area | — |
| `update` | Download, verify and install the newest bravros binary | ✅ |
| `version` | Print version | — |
| `worktree` | Worktree lifecycle management | — |

---

## `setup` ✅

Install bravros components into `~/.claude` from the payload embedded in the binary. No network,
no source checkout. `install.sh` and `install.ps1` `exec` into this as their final act.

```
bravros setup [flags]
```

`Args: cobra.NoArgs`. Interactive when stdin is a TTY; flag-driven otherwise.

### Flags

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--all` | bool | `false` | Install every component. Implies `--skills=all`. |
| `--components` | string | `""` | Comma-separated component ids. Overrides `BRAVROS_COMPONENTS`. |
| `--skills` | string | `""` (⇒ `core`) | Skill scope for `claude-skills`: `core` or `all`. Anything else is an error. |
| `--yes` | bool | `false` | Non-interactive: skip the picker and the confirm screen. |

### Component ids

`cli` (required, `~/.claude/bin`) · `claude-skills` (`~/.claude/skills`, scoped) ·
`claude-templates` (`~/.claude/templates`) · `claude-settings` (`~/.claude/settings.json`,
deep-merged).

`--skills=core` is 18 of 35 skills (`core: true` frontmatter); `--skills=all` is all 35.

### Environment

`BRAVROS_COMPONENTS` · `BRAVROS_INSTALL_METHOD` · `BRAVROS_ALLOW_PLUGIN_MANAGED` ·
`BRAVROS_CONFIG_DIR`. See `docs/cli/install-update.md` for what each does.

### Sample output

First run:

```
bravros setup → /Users/you/.claude

  cli                /Users/you/.claude/bin
      installed by install.sh / install.ps1: /Users/you/.claude/bin/bravros
  claude-templates   /Users/you/.claude/templates
      10 file(s) to write, 0 already up to date

✓ setup complete — 10 written, 0 unchanged, 0 preserved as .new, 0 pruned
  state: /Users/you/.claude/state/setup.json (written)
```

Second, identical run — idempotence is observable in the output, not merely asserted:

```
  claude-templates   /Users/you/.claude/templates
      0 file(s) to write, 10 already up to date

✓ already up to date — no changes (10 file(s) verified)
  state: /Users/you/.claude/state/setup.json (unchanged)
```

### `state.json`

`~/.claude/state/setup.json` (`<config dir>/state/`, on the never-prune allowlist):

```json
{
  "schema": 1,
  "bravros_version": "0.1.0",
  "install_method": "source",
  "claude_root": "/Users/you/.claude",
  "skills_scope": "core",
  "components": [
    { "id": "cli", "target": "bin" },
    { "id": "claude-templates", "target": "templates" }
  ]
}
```

A `claude-skills` entry additionally carries `"scope"` and the **resolved** `"skills": [...]`
list. `install_method` is one of `installer` / `brew` / `scoop` / `source` / `unknown`, or
whatever `BRAVROS_INSTALL_METHOD` declared. There is no timestamp field, deliberately.

### Contract a skill can rely on

- Never destructively overwrites: a differing existing file stays, the payload version lands as
  `<name>.new` and is reported.
- `settings.json` is deep-merged, never replaced.
- Pruning is scoped to `skills/` + `templates/`; `hooks/`, `agents/` and `state/` are untouched.
- A plugin-managed install is detected and refused, never written into.
- Re-running is idempotent.

---

## `selfupdate` ✅

Refresh installed components from **this binary's** embedded payload. The automatic half of the
split update model — this is what the SessionStart hook runs. Its only network traffic is the
rate-limited passive version check — which, on a binary `install.sh` owns
(`install_method: "installer"` in setup.json), is also a trigger: a newer release past the ~6h
canary window is downloaded, minisign-verified and swapped in automatically, keeping the old
executable as `bravros.prev` and printing one `🔄 bravros vX → vY (auto)` line. brew/scoop/source
installs are never swapped (notify-only). Opt out with `BRAVROS_NO_UPDATE_CHECK=1` or
`"auto_update": false` in setup.json.

```
bravros selfupdate [flags]
```

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--force` | bool | `false` | Bypass the check-TTL cache and refresh now. |
| `--dry-run` | bool | `false` | Show what would be updated; change nothing on disk. |
| `--verbose` | bool | `false` | Detailed trace output. |
| `--skip-if-recent` | string | `""` | Skip if the last update was within this duration (`6h`, `30m`, …). |
| `--fetch-payload` | bool | `false` | Legacy network lane: fetch + verify + deploy the published `bravros-payload.tar.gz`. |
| `--silent` | bool | `false` | Deprecated no-op — silence is the default. |
| `--deep` | bool | `false` | Deprecated no-op — the clone-based drift detectors are gone. |

Environment: `BRAVROS_SELFUPDATE_TTL` (default `6h`, `0` disables the whole-run cache) ·
`BRAVROS_NO_UPDATE_CHECK=1` (disables the passive notice and the auto-update lane) ·
`BRAVROS_UPDATE_NOTICE_TTL` (default `24h`) · `BRAVROS_REMOTE_CHECK_TTL` ·
`BRAVROS_MIN_RELEASE_AGE` (default `6h`, the auto-update canary window; `0` disables).

**Trap for skill authors: exit code proves nothing here.** `selfupdate` returns `nil` on nearly
every path, including "did nothing at all" (a TTL cache hit). If a skill needs to know whether
anything happened, observe the filesystem — do not branch on `$?`.

Migration rule: an install with no `state.json` but a populated `~/.claude/skills` is refreshed at
scope `all`, never `core`, so an upgrade never silently deletes skills the user already had.

---

## `update` ✅

Download, verify and install the newest bravros binary. **Its own verb since P-0015 — no longer
an alias for `selfupdate`.** A skill that shells out to `bravros update` expecting a local
skill refresh is now wrong; call `bravros selfupdate` for that.

```
bravros update [flags]
```

`Args: cobra.NoArgs`, `SilenceUsage: true`.

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--check` | bool | `false` | Report whether a newer release exists; replace nothing. |
| `--force` | bool | `false` | Install even when already on the newest version. |
| `--tag` | string | `""` | Install a specific release tag instead of the newest. |

```bash
bravros update             # check, and install if a newer release exists
bravros update --check     # report only, replace nothing
bravros update --force     # reinstall even when already newest
bravros update --tag v3.2.0
```

Sequence: resolve newest release → download this platform's archive → verify against the
minisign-signed `checksums.txt` → atomically replace the running executable → run the new
binary's `selfupdate --force`.

**It refuses when a package manager owns the binary**, naming the right command instead (e.g.
`brew upgrade bravros`). Observed reality wins over `state.json`: a binary under `/Cellar/`,
`/homebrew/`, `/linuxbrew/` or `/scoop/` is refused regardless of the record, and a missing record
is never by itself grounds to refuse. The refusal is not a usage error — usage is silenced so the
one line naming the correct command is not buried.

Platform note: on POSIX the replace is a single atomic `rename(2)`. On Windows the running image
is locked, so the current binary is renamed aside first and the leftover `bravros.exe.old-<rand>`
is swept on a later run.
