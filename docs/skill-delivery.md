# Skill Delivery — How skills reach your agent

Skills reach your machine one of two ways. **Path A is the CLI**, and it is the primary path:
skills ship *inside* the binary, so installing them is one signed download and refreshing them
needs no network at all. **Path B is a plugin host** — Claude Code's marketplace or the Gemini
CLI extension — which fetches skills from the public repository on its own schedule and owns the
tree itself.

Never point both at the same tree. The binary refuses to write into a plugin-managed directory
(`deploy.IsPluginManaged`), which is a guard, not a merge strategy.

## Quick reference

| Host | Path | Install | Updates |
|---|---|---|---|
| **Claude Code (CLI, recommended)** | A | `bash -c "$(curl -fsSL https://install.bravros.dev)"` then `bravros setup` | `bravros selfupdate` every session, from the embedded payload — no network. New binaries via `bravros update`. |
| **Claude Code (plugin marketplace)** | B | `/plugin marketplace add bravros/bravros` then `/plugin install bravros` | Claude Code's own background refresh |
| **Gemini CLI** | B | `gemini extensions install https://github.com/bravros/bravros --auto-update` | Gemini's own extension system, on launch |

Any other host that reads `~/.claude/skills` is served by Path A. The per-harness adapters
(Codex/OpenCode/Pi) and the per-host skills compiler were retired in P-0187; `bravros install`
now exists only to point at `install.sh`.

---

## Path A — the CLI, with the payload embedded in the binary

### Installation

```bash
bash -c "$(curl -fsSL https://install.bravros.dev)"          # macOS / Linux / WSL
irm https://install.bravros.dev/install.ps1 | iex            # native Windows (degraded tier)
```

The installer's only job is to put a **verified** binary at `~/.claude/bin/bravros`
(`%USERPROFILE%\.claude\bin\bravros.exe` on Windows) and then `exec bravros setup`. Every
question and every write into your home directory belongs to the Go binary — the shell script
deliberately knows nothing about components.

`bravros setup` then writes the components you chose from the payload **embedded in the binary**.
There is no second download, no clone, and no GitHub token at any point.

### The four components

One selection axis. There is no plugin-category picker: the marketplace's category plugins are a
generated *view* of the same skill files, and exposing both axes would let them disagree.

| id | Kind | Target | What it is |
|---|---|---|---|
| `cli` | binary | `~/.claude/bin` | The bravros binary itself. Required — everything else is driven by it, and the installer has already placed it. |
| `claude-skills` | embedded tree | `~/.claude/skills` | The agent skills. Carries a **scope**: `core` (default) or `all`. |
| `claude-templates` | embedded tree | `~/.claude/templates` | Git hooks and project templates used by `bravros init` and the commit-msg gate. |
| `claude-settings` | merged settings | `~/.claude/settings.json` | The managed block (the SessionStart hook), deep-merged into any existing file — never overwritten. |

Targets are stored as path *segments* and joined at use time, so Windows resolves the same layout
under `%USERPROFILE%` with no hand-written backslash and no `%LOCALAPPDATA%` special case.

### Skill scope — `core` vs `all`

`claude-skills` is the one scoped component.

- **`core` (default)** — the skills whose `SKILL.md` frontmatter carries `core: true`. That is
  **18 of 35** today, and the set is asserted by test to be identical *by name* to the
  marketplace's core plugin (`plugins/core/skills/`).
- **`all`** — every embedded skill (35).

`core` is the default for a **fresh** run on purpose: an always-on skill list has a context
budget, and blowing it silently hides your least-used skills.

**Migration rule — an upgrade never silently removes skills.** An install that predates this
mechanism has no `state.json` but does have a populated `~/.claude/skills`. Against such a
machine the refresh runs at scope **`all`**, because that is exactly what the machine already
had; defaulting to `core` there would delete every non-core skill the user was relying on.
`core` applies only to a fresh wizard run.

### Flags and environment

```bash
bravros setup                                                    # interactive when stdin is a TTY
bravros setup --all --yes                                        # every component; implies --skills=all
bravros setup --components=claude-skills,claude-settings --yes   # a subset
bravros setup --skills=all --yes                                 # every skill, default components
BRAVROS_COMPONENTS=claude-skills bravros setup --yes             # component list from the environment
```

| Flag / variable | Effect |
|---|---|
| `--all` | Install every component. Implies `--skills=all`. |
| `--components=<a,b>` | Comma-separated component ids. Overrides `BRAVROS_COMPONENTS`. |
| `--skills=<core\|all>` | Skill scope for `claude-skills`. Default `core`. |
| `--yes` | Non-interactive: skip the picker and the confirm screen. |
| `BRAVROS_COMPONENTS` | Component list honored when `--components` is absent. |
| `BRAVROS_INSTALL_METHOD` | Set by `install.sh` / `install.ps1` to record how the binary arrived. Read later by `bravros update`. |
| `BRAVROS_CONFIG_DIR` | Overrides `~/.claude` as the install root. |
| `BRAVROS_ALLOW_PLUGIN_MANAGED` | Escape hatch for the plugin-managed refusal. An environment variable rather than a flag on purpose — the safe path must be the short one. |

### Non-destructive by contract

- A file that already exists and **differs** is never overwritten. It stays exactly as it is and
  the payload's version is written beside it as `<name>.new`, then reported in the summary.
- `settings.json` is deep-merged entry by entry through `cli/internal/managed/`, never replaced.
- Pruning is scoped to `skills/` and `templates/`. `~/.claude/hooks/`, `~/.claude/agents/` and
  `~/.claude/state/` are never touched.
- A plugin-managed Claude Code install is **detected and warned about**; nothing is written into
  a directory a plugin host owns.
- Re-running is **idempotent**: a second run with the same selection reports no changes.

### `state.json`

The record of what was installed lives at `~/.claude/state/setup.json` (`<config dir>/state/`,
which is on the never-prune allowlist, so neither `deploy` nor `selfupdate` can remove it):

```json
{
  "schema": 1,
  "bravros_version": "v2.11.0",
  "install_method": "installer",
  "claude_root": "/Users/you/.claude",
  "skills_scope": "core",
  "components": [
    { "id": "claude-skills", "scope": "core", "skills": ["commit", "..."], "target": "skills" }
  ]
}
```

Two things about it are deliberate. It stores the **resolved skill list**, not just the scope, so
a later release that moves the core/all split can still tell exactly what this machine has. And
it carries **no timestamp**, so two identical runs produce byte-identical state — which is what
makes "the second run changed nothing" observable rather than merely asserted.

This is also the first machine-wide home for the choice. `.bravros.yml`'s `skills.enabled` is
resolved starting from the current working directory, so it is per-project by construction — your
"I only want core skills" preference used to depend on which repo you happened to be standing in.

---

## The split update model

Two verbs, split by risk. This is the single most important thing to understand about updates.

| | `bravros selfupdate` | `bravros update` |
|---|---|---|
| **Who runs it** | The SessionStart hook, automatically | You, explicitly |
| **Source** | The payload **embedded in the binary you already have** | The newest published GitHub release |
| **Network** | None — except one passive version check, at most once per 24h | Yes: archive + `checksums.txt` + `checksums.txt.minisig` |
| **Replaces the binary** | Never | Yes, atomically |
| **Failure mode** | Cannot leave the machine half-updated; it is a local file copy | Rolls the previous binary back into place |

`update` was previously just an alias for `selfupdate`. It is now its own command, and the alias
is gone — a test asserts both halves, because leaving the alias alongside the new command would
make cobra's resolution order-dependent and the bug silent.

### `bravros selfupdate` — the automatic half

Registered by the `claude-settings` component as a SessionStart hook in `~/.claude/settings.json`.
On macOS and Linux the hook is wrapped in a guard that makes it a no-op inside the Claude desktop
app; the Windows form has no such guard (`cmd.exe` cannot parse a shell `case`, and the
`__CFBundleIdentifier` condition is macOS-only).

What it does, in order:

1. **TTL cache first.** After each completed run, `~/.claude/state/.bravros-last-check` is
   stamped. A run inside the TTL returns immediately having done *nothing at all* — this is what
   keeps the hook near-free. Default 6h, override with `BRAVROS_SELFUPDATE_TTL` (`0` disables),
   bypass with `--force`.
2. **Refresh from the embedded payload.** Whatever `state.json` records is re-extracted from the
   binary, honoring every non-destructive rule above. No `state.json` but a populated
   `~/.claude/skills` ⇒ scope `all` (the migration rule).
3. **Passive notice.** At most once every 24h it checks whether a newer bravros was published and
   prints one line saying so. It never blocks, never fails the run, and never replaces anything.
   `BRAVROS_NO_UPDATE_CHECK=1` turns it off; `BRAVROS_UPDATE_NOTICE_TTL` changes the interval.

Useful flags: `--force` (bypass the TTL cache), `--dry-run` (report without touching disk),
`--verbose` (trace output; silence is the default), `--skip-if-recent=<duration>`.
`--fetch-payload` selects the legacy network lane — resolve the newest release, download and
minisign-verify `bravros-payload.tar.gz`, and deploy that instead of the embedded payload. It is
opt-in precisely because the SessionStart hook was moved off the network.

### `bravros update` — the explicit half

```bash
bravros update             # check, and install if a newer release exists
bravros update --check     # report only, replace nothing
bravros update --force     # reinstall even when already on the newest version
bravros update --tag v3.2.0
```

It resolves the newest release, downloads the archive for this platform, verifies it against the
minisign-signed `checksums.txt`, atomically replaces the running executable, and then runs the
**new** binary's `selfupdate --force` so your components are refreshed from the payload embedded
in it.

On POSIX the replace is a single `rename(2)`: atomic, with the running process keeping its
now-unlinked inode, so there is never an instant with no binary on disk. Windows locks a running
image against replacement, so there the current binary is renamed aside first and the sideline
(`bravros.exe.old-<rand>`) cannot be deleted by the process still running it — it is swept on a
later run. That asymmetry is the reason native Windows is a documented degraded tier.

**It refuses when a package manager owns the binary.** Replacing a brew- or scoop-managed file
would leave that package manager's metadata pointing at something it no longer controls, and its
next upgrade would silently roll you back. The refusal names the right command
(`brew upgrade bravros`). Observed reality wins over the record: a missing `state.json` is never
by itself grounds to refuse.

### Deploying a payload by hand

```bash
bravros deploy --source /path/to/payload    # must contain skills/ (and optionally templates/)
bravros deploy --dry-run                    # list what would be written
```

---

## Path B — plugin hosts (Claude Code marketplace, Gemini CLI)

Claude Code and Gemini CLI have built-in plugin/extension systems that fetch skills directly from
the public repository. No local clone, and no bravros binary involved in the skill tree.

**Claude Code**

```
/plugin marketplace add bravros/bravros
/plugin install bravros
```

Updates happen in the background. You may see a refresh notification; no action is required.
Install the core plugin alone, or add the category plugins (`bravros-sdlc`, `bravros-design`,
`bravros-deploy`, `bravros-tools`) for the long tail.

**Gemini CLI**

```bash
gemini extensions install https://github.com/bravros/bravros --auto-update
```

The extension is fetched when you launch Gemini CLI, which also checks for updates on each
launch.

**How it works.** These hosts pull skills from the public GitHub repository through their own
mechanisms. The bravros binary never writes into their directories (`~/.claude/plugins/`,
`.claude-plugin/`, `~/.gemini/extensions/`); if it tried, `deploy.IsPluginManaged` fails the
operation loudly — two writers on one skill tree is a conflict, not redundancy. Path B is
unaffected by `bravros selfupdate`, by release cadence, and by everything else in this document.

---

## Trust and security

The chain is the same for the installer, for `bravros update`, and for the legacy
`--fetch-payload` lane:

1. The minisign public key is **compiled into the binary as a constant** and pinned as a literal
   in `install.sh` and `install.ps1`. There is no key file on disk to tamper with, and no key is
   ever downloaded.
2. Each release signs `checksums.txt` with the corresponding private key, held only in CI. Every
   artifact is covered because its SHA-256 line lives inside that signed file — the 6 archives,
   `bravros-payload.tar.gz`, `install.sh` and `install.ps1` included.
3. The signature over `checksums.txt` is verified **before a single byte of it is trusted**. Only
   then is a downloaded artifact's SHA-256 compared, and only then is anything extracted.
4. A tampered, truncated, or unsigned artifact is refused. Extraction happens into a staging
   directory and is swapped into place atomically, so a failure at any step leaves the previous
   tree exactly as it was.
5. Archive entries are bounded and sanitised: path traversal, absolute paths and symlink entries
   are rejected outright.

On native Windows, minisign itself has to be bootstrapped: `install.ps1` downloads a **pinned**
`minisign.exe` from jedisct1's official win64 release and checks it against a SHA-256 pinned in
the script before using it. The Go-binary alternative — having `bravros.exe` verify its own
release manifest — was rejected as circular: the artifact under suspicion would be attesting to
itself. The pinned download fails closed; a stale pin aborts rather than degrading to no
verification, at the cost of needing a bump on each minisign release.

Verify the pinned key yourself against the one published at
[bravros.dev/security](https://bravros.dev/security) — it appears in `install.sh`, `install.ps1`
and `cli/internal/fetch/fetch.go`, and all three must match.

---

## Offline behavior

The SessionStart lane is a local file copy, so being offline changes nothing about it. The
passive version check fails silently: no error, no blocking, exit code 0, and nothing is retried
until the next session. `bravros update` obviously needs the network, and reports plainly when it
cannot reach GitHub — leaving the binary you already have exactly where it was.

---

## Why the split?

The risky work and the safe work used to be the same command running on a hook. Refreshing files
from a payload you already hold cannot leave a machine half-updated; downloading and replacing a
running executable can. So the safe half stays automatic and free, and the half that can go wrong
became something you type.
