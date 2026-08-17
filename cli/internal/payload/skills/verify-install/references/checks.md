# verify-install — the check list

Everything below is implemented in `scripts/verify.sh`. Read this when you need to
know *what* a line of output means, or before adding a check.

## Checks, in run order

| # | Check | Signal | Fix |
|---|---|---|---|
| 1 | `~/.bravros/bin/bravros` exists, is executable, and `bravros version` runs | binary health | `bash $PORTABLE_REPO/install.sh` |
| 2 | `~/.bravros/bin` persisted in a shell RC | skills and hooks call `bravros` bare; an exported PATH does not survive the session | append the export (auto-fixed) |
| 3 | `skills/ hooks/ scripts/ templates/ cache/ bin/` present under `~/.bravros`, templates non-empty | runtime layout | `mkdir -p` (auto-fixed) |
| 4 | Deploy manifest present at `~/.bravros/skills/.deploy-manifest.json` | absence forces a full re-copy on next deploy | `bravros deploy` |
| 5 | Per-skill content digest, source vs deployed | **the drift signal** | `bravros deploy --force --filter <skill>` |
| 6 | Skill in source but not deployed | missing deploy | same |
| 7 | Skill deployed but retired from source | orphan — its triggers still fire | `--fix` prunes |
| 8 | `skills/shared` / `skills/_shared` deployed | install-hygiene failure (repo-only material) | `--fix` removes it |
| 9 | `~/.bravros/CLAUDE.md` managed block vs `home/CLAUDE.md` | managed-block drift | `scripts/reconcile-global-claude.py` |
| 10 | `~/.bravros/settings.json` present + valid JSON; a locked file reports healthy | config presence | restore from `config/settings.json` |
| 11 | `templates/.githooks/commit-msg` deployed and carrying the `bravros-managed-commit-msg-hook` marker | commit-format gate | `bravros hooks update --force` |
| 12 | `hooks/*.{sh,py}` and `scripts/*.{sh,py}`, source vs deployed (md5) | file drift | `cp -f` (auto-fixed) |
| 13 | Every `mcpServers` key in `config/mcp.json` registered in `~/.bravros.json` | MCP registration | `bravros mcp register --from config/mcp.json` |
| 14 | `bravros doctor --quick --json` | gh / jq / curl / git / `~/.bravros` — **delegated, never re-implemented here** | per-check `fix_hint` |
| 15 | `bravros skills deps --format json`, each `check_cmd` run | missing per-skill dependency | the per-OS `install_cmd_*`, offered never applied |

## Why the digest is source-vs-deployed

`deploy.ComputeSkillSHA` resolves symlinks, and `bravros deploy` materializes a
source skill's `references/*.md` symlinks with `cp -RL`. A clean deploy therefore
yields byte-identical digests on both sides (verified 2026-08-13), which makes a
direct source→runtime comparison valid *and* symlink-safe.

Comparing the deployed tree against the **manifest** instead is the trap the old
script fell into: the manifest records what was deployed, so a source edit that was
never deployed reports a cheerful `match`. Source-vs-deployed catches it.

## `--auto` output line shapes

Silence means healthy. Otherwise, one line per finding:

```
BINARY: <missing|not-executable|exec-failed> — <path>
PATH: ~/.bravros/bin absent from every shell RC
LAYOUT: missing <path>
MANIFEST: absent — run bravros deploy
SOURCE_REPO: missing at <path>
SKILL_DRIFT: <name> — bravros deploy --force --filter <name>
SKILL_MISSING: <name> — bravros deploy --force --filter <name>
SKILL_ORPHAN: <name>
SHARED_LEAK: <path>
CLAUDE_MD: managed block drift (<reason>)
SETTINGS: <missing|invalid JSON>
HOOK_TEMPLATE: <missing|marker absent|drift> [— <fix>]
FILE_MISSING: <path>
FILE_DRIFT: <path>
MCP_MISSING: <name>[, <name>…]
DOCTOR_STATUS: <healthy|degraded|critical>
DOCTOR_CHECK: <name> — <status> [— <message>] [— fix: <cmd>]
MISSING_SKILL_DEP: <name> — <install_cmd>
SUMMARY: <n> pass, <n> fail, <n> warn, <n> intentional
```

Exit code is 0 unless there is at least one failure. Warnings (orphans, missing
deps) do not fail the run.

## What this skill deliberately does NOT check

Each of these was removed on 2026-08-13 (P-0187) with a reason — do not re-add
without one:

- **1Password / HASS / Firecrawl / Context7 injections.** Secret-bearing checks
  cannot run from a SessionStart hook, and merging the auto- and manual variants
  means one behaviour. `bravros doctor` (without `--quick`) still checks the `op`
  binary.
- **Codex / OpenCode / Pi runtime verification.** The per-host skills compiler was
  retired; skills are host-neutral now, so there is no per-host build to verify.
- **`settings.json` byte-comparison against `config/settings.json`.** The operator
  legitimately edits it. A permanent red trains you to ignore the report.
- **Binary-staleness rescue against the GitHub releases API.** `bravros selfupdate`
  owns that on SessionStart and is the auto-deploy mechanism for every machine.
- **A bash/python re-implementation of the skill digest.** See the hard constraint
  in `SKILL.md`.
- **`gh` / `jq` / `curl` / `git` presence.** `bravros doctor --quick` owns them.
- **Claude-in-Chrome preference flags, MacStudio announce script, ad-hoc external
  tool list** (`uv`, `lighthouse`, `notebooklm`, …). Per-machine concerns, not
  install integrity.
