# `bravros` CLI — index

The SDLC kernel binary: atomic, destructive, token-gated and headless primitives. Everything the
agent can be trusted to improvise lives in a skill; everything it cannot lives here.

- **Skill-author reference:** [`../example-bravros-cli.md`](../example-bravros-cli.md) — treat it
  as a public API contract. If it is stale, skills break.
- **Deep-dives:** `docs/cli/<group>.md`, listed below.
- **Contributor rule:** a new verb, a renamed flag, or a changed output shape must be reflected in
  `example-bravros-cli.md`, this index, and the right deep-dive **in the same PR**
  (`cli/CLAUDE.md` § Docs-sync requirement). `bravros audit-docs`, the CI drift-linter that used
  to check the flag tables, was retired with the audit engine in P-0187 — nothing checks this
  automatically. Verify by hand against `cli/cmd/*.go` `Use:`/flag definitions.

## Deep-dives

| Group | File | Covers |
|---|---|---|
| Install & update | [`cli/install-update.md`](cli/install-update.md) | `setup`, `selfupdate`, `update`, `deploy`, `install` |

Other groups have no deep-dive yet; use `bravros <verb> --help`, which is generated from the same
`cli/cmd/*.go` definitions and is therefore never stale.

## All verbs

| Verb | What it does | Deep-dive |
|---|---|---|
| `autopr` | Manage autonomous pipeline lock | — |
| `branch` | Branch management utilities (incl. `branch prune`) | — |
| `clean-untracked` | Preserve into `.trash/` then remove untracked files (no token) | — |
| `commit` | Commit plan + code changes (format-enforcing) | — |
| `completion` | Generate the shell autocompletion script | — |
| `config` | Read project configuration from `.bravros/config.json` (`skills.preserve`, `staging_branch`) | — |
| `deploy` | Deploy the toolkit runtime into the host config dir | [install-update](cli/install-update.md) |
| `destructive` | Gate permanently destructive commands with a human-presence token | — |
| `discard` | Preserve into `.trash/` then discard uncommitted changes (no token) | — |
| `doctor` | Run health checks on the bravros installation | — |
| `ha` | Home Assistant CLI (announcements) | — |
| `hook` | Claude Code hook subcommands | — |
| `hooks` | Manage bravros-managed git hooks | — |
| `init` | Initialize the current repository with the SDLC structure | — |
| `install` | Install the toolkit runtime (delegates to `install.sh`) | [install-update](cli/install-update.md) |
| `merge-lock` | Atomic merge-lock primitive (`acquire` / `release` / `status`) | — |
| `nextid` | Atomically reserve next plan, backlog, report and user-report IDs (JSON) | — |
| `pr-review` | Write the PR review stamp from the latest bot verdict (`--write-stamp`) | — |
| `promote` | Promote `homolog` → `main` with a human-presence token | — |
| `secrets` | Manage bravros secrets (`op` / `env` / `none` backends) | — |
| `selfupdate` | Refresh installed components from this binary's embedded payload | [install-update](cli/install-update.md) |
| `setup` | Install bravros components from the embedded payload | [install-update](cli/install-update.md) |
| `statusline` | Render the Claude Code status line (reads JSON from stdin) | — |
| `trash` | Inspect and manage the `.trash/` preserve area | — |
| `update` | Download, verify and install the newest bravros binary | [install-update](cli/install-update.md) |
| `version` | Print version | — |
| `worktree` | Worktree lifecycle management | — |

## Notes that bite

- **`update` is not an alias for `selfupdate`.** They are two verbs (the old alias was removed
  in P-0015), but as of P-0018 the risk split is conditional, not absolute: `selfupdate` refreshes
  components from this binary's embedded payload AND, on a binary that `install.sh` owns
  (`install_method: "installer"` in setup.json), its 24h SessionStart check now downloads,
  verifies and **replaces the binary itself** — keeping the outgoing one as `bravros.prev` and
  printing one `🔄 bravros vX → vY (auto)` line. brew/scoop/source installs are never swapped
  (notify-only). `update` remains the explicit, on-demand binary replace. Opt out of the auto
  lane with `BRAVROS_NO_UPDATE_CHECK=1` or `"auto_update": false` in setup.json; releases
  younger than the `BRAVROS_MIN_RELEASE_AGE` canary window (default `6h`) are deferred. See
  [install-update](cli/install-update.md).
- **Exit code proves nothing for `selfupdate`** — it returns `nil` on nearly every path including
  "did nothing". Verify by observing the filesystem.
- **`bravros setup` ≠ `bravros worktree setup`.** Different commands; the top-level verb does not
  shadow the subcommand.
- **`doctor --json` is SILENT when healthy** — empty stdout, by design; that is the SessionStart-hook
  contract (`cli/cmd/doctor.go:33,74`). Do not read "no output" as "did not run". Its other flags
  are `--quick`, `--deep`, `--install-missing`, `--fix`. Exit code `0` covers **both** healthy and
  degraded-with-warnings (`doctor.go:37`), so the exit code alone does not tell you the tree is
  clean — parse the JSON or read the human output.
