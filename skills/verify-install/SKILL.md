---
name: verify-install
description: >
  Post-installation verification for the Skaisser SDLC 4.0 portable setup. Compares the portable repo
  ($PORTABLE_REPO — ~/Sites/claude on macOS, ~/claude on Linux) against the deployed installation
  (~/.claude) to catch drift, missing files, stale skills, broken 1Password injections, and
  misconfigured settings. Use this skill whenever the user says "verify install", "check install",
  "verify setup", "is everything installed", "check my setup", "installation health check",
  "did install work", or any variation of checking whether install.sh ran correctly. Also trigger
  when the user mentions problems after running install.sh, or wants to confirm their Claude Code
  portable environment is in sync. Even a casual "is my stuff up to date?" or "something feels
  broken after install" should trigger this skill.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# verify-install

Verifies that `install.sh` from the portable repo (`$PORTABLE_REPO` — `~/Sites/claude` on macOS, `~/claude` on Linux) deployed everything correctly to `~/.claude/`. Produces a colored terminal report grouped by category, with inline pass/fail status and auto-fix suggestions when something is wrong.

## Model Requirement

**Sonnet 4.6** — this skill performs mechanical/scripted operations that don't require deep reasoning.

## When to use

Run this after `install.sh` completes, after pulling new changes to the portable repo, or any time
something feels off with the Claude Code setup. It replaces the manual "eyeball the output and hope
nothing broke" approach.

## How it works

The verification script (`scripts/verify.sh` bundled with this skill) performs these check categories:

### 1. Skills Integrity (MD5 comparison)
Compares every file inside each skill directory (SKILL.md, references/*, scripts/*) between
`$PORTABLE_REPO/skills/` and `~/.claude/skills/` using MD5 checksums. Reports:
- **Match**: source and deployed are identical
- **Mismatch**: file content differs (stale deployment)
- **Missing in deployed**: source skill wasn't copied
- **Orphaned**: deployed skill that no longer exists in source (should have been cleaned up)

### 2. Config Files (MD5 comparison)
Compares these config files between source and deployed:
- `settings.json` (config/settings.json vs ~/.claude/settings.json)
- `mcp.json` (config/mcp.json vs ~/.claude/mcp.json) — note: mcp.json is patched post-copy
  (Herd npx paths, 1Password secrets), so a mismatch is expected. The script checks structural
  integrity (valid JSON, expected keys present) rather than byte-for-byte match.
- `CLAUDE.md` (CLAUDE.md vs ~/.claude/CLAUDE.md)

### 3. Settings Validation
Parses `~/.claude/settings.json` and verifies:
- `hooks.PreToolUse` contains the `bravros audit` hook
- `hooks.SessionStart` contains `telegram-patch.sh` and `bravros update`
- `statusLine` points to `bravros statusline`
- `enabledPlugins` has `ralph-loop` and `telegram`
- `permissions.defaultMode` is `dontAsk`
- `env.CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` is `"1"`

### 4. bravros Binary
- Checks `~/.claude/bin/bravros` exists and is executable
- Compares MD5 of deployed binary against the platform-correct source binary
  (`cli/bravros-<os>-<arch>`)
- Runs `bravros version` to confirm it executes

### 5. 1Password Injections
Verifies secrets were actually injected into their target files (not just that `op` is installed):
- **Context7 API key**: `~/.claude/mcp.json` → `mcpServers.context7.env.CONTEXT7_API_KEY` is not
  `__OP_INJECT__` or empty
- **Telegram bot token**: `~/.claude/channels/telegram/.env` contains `TELEGRAM_BOT_TOKEN=` with a
  non-empty value
- **Home Assistant**: shell RC file contains `HASS_SERVER` and `HASS_TOKEN` exports with non-empty values

### 6. Directory Structure & Hooks
- Required directories exist: skills/, hooks/, scripts/, templates/, cache/, bin/, channels/telegram/
- Hook files are executable: `telegram-patch.sh`
- `~/.claude/bin` is in PATH (checks shell RC)

### 7. External Tools
- `op` (1Password CLI) is available
- `uv` (Astral) is available
- `firecrawl` CLI is available
- `pipx` is available
- `hass-cli` is available
- `notebooklm` is available
- Plugins: `ralph-loop` and `telegram` in installed_plugins.json

## Running the verification

```bash
bash <skill-dir>/scripts/verify.sh
```

The script auto-detects `PORTABLE_REPO` (defaults to `~/Sites/claude` on macOS, `~/claude` on Linux) and `DEPLOYED_DIR`
(defaults to `~/.claude`). Override with env vars if needed:

```bash
PORTABLE_REPO=/path/to/repo DEPLOYED_DIR=/path/to/deployed bash scripts/verify.sh
```

## Output format

The output is grouped by category with colored status indicators:

```
══════════════════════════════════════════════════════════
  Skaisser SDLC 4.0 — Installation Verification
══════════════════════════════════════════════════════════

━━ Skills Integrity ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ✅ backlog                    match
  ✅ branch                     match
  ❌ commit                     MISMATCH (SKILL.md differs)
     ↳ Fix: cp -rf ~/Sites/claude/skills/commit ~/.claude/skills/
  ⚠️  old-skill                  ORPHANED (not in source)
     ↳ Fix: rm -rf ~/.claude/skills/old-skill

━━ 1Password Injections ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ✅ Context7 API Key           injected
  ❌ Telegram Bot Token         MISSING
     ↳ Fix: op read "op://HomeLab/Telegram Bot Token/password"
            then write to ~/.claude/channels/telegram/.env
```

At the bottom, a summary line:

```
━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  62 checks passed, 2 failed, 1 warning
  Run with --fix to auto-fix all issues
```

## Auto-fix mode

When run with `--fix`, the script will:
1. Re-copy any mismatched skills from source
2. Re-copy mismatched config files (settings.json, CLAUDE.md)
3. Re-run 1Password injection commands for missing secrets
4. Remove orphaned skills
5. Fix missing directory structure
6. Fix file permissions (hooks, CLI binary)

Each fix is printed as it happens so you can see exactly what changed.

## Instructions for Claude

When the user triggers this skill:

## Step 1/3: Run Verification Script

```bash
echo "✨ verify-install [1/3] Running installation verification"
bash ~/.claude/skills/verify-install/scripts/verify.sh
```

If the portable repo itself is missing or not a git repo, suggest:
```bash
# macOS
git clone git@github.com:skaisser/claude.git ~/Sites/claude
cd ~/Sites/claude && bash install.sh
# Linux
git clone git@github.com:skaisser/claude.git ~/claude
cd ~/claude && bash install.sh
```

## Step 2/3: Analyze Results

```bash
echo "✨ verify-install [2/3] Analyzing check results"
```

Read the output carefully. If everything passes, confirm to the user that their installation is healthy. If there are failures, show the user the categorized output.

## Step 3/3: Fix Issues (if any)

```bash
echo "✨ verify-install [3/3] Applying fixes and confirming green state"
```

- Ask if they want to auto-fix: `bash ~/.claude/skills/verify-install/scripts/verify.sh --fix`
- If auto-fix isn't appropriate (e.g., 1Password isn't authenticated), explain the manual steps
- After fixing, re-run verification to confirm everything is green.
