---
name: obsidian-setup
description: >
  Set up Obsidian vault with coding symlinks and plugin configs for multi-machine deployment.
  Use this skill whenever the user says "/obsidian-setup", "setup obsidian", "configure obsidian",
  or any request to set up the Obsidian vault for coding projects.
  Also triggers on "obsidian vault", "link planning to obsidian", "obsidian init".
  Supports `add <project>` to add a single project symlink.
---

# Obsidian Setup: Vault Configuration for Coding Projects

Set up the Obsidian vault with symlinks to all project `.planning/` directories and deploy canonical plugin configs from templates. macOS only.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Critical Rules

1. **macOS only.** All paths assume macOS (iCloud, `~/Library/`).
2. **Idempotent.** Safe to re-run — skips existing correct symlinks and unchanged configs.
3. **Never delete.** Only creates directories, symlinks, and copies configs. Never removes anything.
4. **Quote all paths.** The iCloud vault path contains spaces — always double-quote.
5. **Templates are optional.** If `~/.claude/templates/obsidian/` doesn't exist, warn but don't fail.

## Constants

```
VAULT_PATH="$HOME/Library/Mobile Documents/com~apple~CloudDocs/Sync/obsidian"
TEMPLATES_PATH="$HOME/.claude/templates/obsidian"
SITES_PATH="$HOME/Sites"
```

## Argument Parsing

Check `$ARGUMENTS` for the `add <project>` subcommand:

- If `$ARGUMENTS` matches `add <project-name>`:
  - Jump directly to **Step 3b** (single project symlink)
  - Skip Steps 1/5, 2/5, 4/5, and 5/5
  - Report success/failure for the single symlink only
- Otherwise: run the full setup (Steps 1/5-5/5)

## Step 1/5: Detect or Create Vault

```bash
echo "📓 obsidian-setup [1/5] detecting vault"
```

1. Check if `"$VAULT_PATH"` exists
   - If yes: report "Vault found at ..."
   - If no: create it with `mkdir -p "$VAULT_PATH"`
2. Create subdirectories if they don't exist:
   ```bash
   mkdir -p "$VAULT_PATH/coding"
   mkdir -p "$VAULT_PATH/personal"
   ```
3. Report vault status

## Step 2/5: Scan for Projects

```bash
echo "📓 obsidian-setup [2/5] scanning for projects with .planning/"
```

1. Find all projects with `.planning/` directories:
   ```bash
   ls -d "$SITES_PATH"/*/.planning/ 2>/dev/null
   ```
2. Extract project names from paths (basename of parent directory)
3. Report total count: "Found {N} projects with .planning/ directories"
4. If no projects found, report and skip to Step 4

## Step 3/5: Create/Update Symlinks

```bash
echo "📓 obsidian-setup [3/5] creating symlinks"
```

For each project found in Step 2:

1. Define target: `"$VAULT_PATH/coding/<project>"`
2. Define source: `"$SITES_PATH/<project>/.planning/"`
3. Check current state:
   - **Symlink exists and points to correct target:** skip, count as "already set up"
   - **Symlink exists but points to wrong target:** remove and recreate, count as "updated"
   - **Regular file/dir exists at target:** warn and skip (don't overwrite real files)
   - **Nothing exists:** create symlink, count as "new"
4. Create symlink:
   ```bash
   ln -sf "$SITES_PATH/<project>/.planning/" "$VAULT_PATH/coding/<project>"
   ```

Track counts: new, already set up, updated, skipped (with warnings).

### Step 3b: Single Project Symlink (add subcommand)

When `$ARGUMENTS` is `add <project-name>`:

1. Validate project name contains only safe characters:
   ```bash
   [[ "$PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Invalid project name: $PROJECT"; exit 1; }
   ```
2. Validate `"$SITES_PATH/<project>/.planning/"` exists — abort if not
3. Create symlink: `ln -sf "$SITES_PATH/<project>/.planning/" "$VAULT_PATH/coding/<project>"`
4. Verify symlink resolves: `test -L "$VAULT_PATH/coding/<project>" && test -d "$VAULT_PATH/coding/<project>"`
5. Report result:
   - Success: "Linked <project> to Obsidian vault"
   - Already exists: "<project> already linked (skipped)"
   - Failure: "Failed to create symlink for <project>"
6. **Stop here** — do not continue to Steps 4 or 5

## Step 4/5: Copy Plugin Configs from Templates

```bash
echo "📓 obsidian-setup [4/5] deploying plugin configs"
```

1. Check if `"$TEMPLATES_PATH"` exists
   - If not: warn "No templates found at ~/.claude/templates/obsidian/ — skipping config deployment" and skip to Step 5
2. Create `.obsidian/` in vault if it doesn't exist:
   ```bash
   mkdir -p "$VAULT_PATH/.obsidian/plugins"
   ```
3. Copy root configs from templates to vault `.obsidian/`:
   - `app.json`
   - `core-plugins.json`
   - `community-plugins.json`
   - `graph.json`
   - `hotkeys.json`
   - `appearance.json`

   For each file:
   - If template file doesn't exist: skip silently
   - If target doesn't exist: copy, count as "new"
   - If target exists and differs from template: copy, count as "updated"
   - If target exists and matches template: skip, count as "already set up"

   **Never copy `workspace.json`** — it's machine-specific state.

4. Copy plugin data configs from `"$TEMPLATES_PATH/plugins/"` to `"$VAULT_PATH/.obsidian/plugins/"`:
   - `plugins/dataview/data.json`
   - `plugins/obsidian-linter/data.json`
   - `plugins/obsidian-kanban/data.json`
   - `plugins/obsidian-icon-folder/data.json`

   For each plugin config:
   - Create the plugin directory if needed: `mkdir -p "$VAULT_PATH/.obsidian/plugins/<plugin-name>"`
   - Apply the same new/updated/already set up logic as root configs

5. Track and report config deployment counts

## Step 5/5: Verify and Report

```bash
echo "📓 obsidian-setup [5/5] verification report"
```

1. Verify each symlink resolves:
   ```bash
   for link in "$VAULT_PATH"/coding/*/; do
     if [ -L "${link%/}" ] && [ -d "$link" ]; then
       # valid
     else
       # broken
     fi
   done
   ```

2. Output a status table:

   ```
   ╔════════════════════════════════════════════════════════════════╗
   ║  Obsidian Setup Report                                       ║
   ╠════════════════════════════════════════════════════════════════╣
   ║  Vault: ~/Library/.../Sync/obsidian/                         ║
   ╠════════════════════════════════════════════════════════════════╣
   ║  Symlinks                                                    ║
   ║  ├─ New:             {N}                                     ║
   ║  ├─ Already set up:  {N}                                     ║
   ║  ├─ Updated:         {N}                                     ║
   ║  └─ Broken/skipped:  {N}                                     ║
   ╠════════════════════════════════════════════════════════════════╣
   ║  Configs                                                     ║
   ║  ├─ New:             {N}                                     ║
   ║  ├─ Already set up:  {N}                                     ║
   ║  └─ Updated:         {N}                                     ║
   ╚════════════════════════════════════════════════════════════════╝
   ```

3. Per-project symlink detail:

   ```
   ┌──────────────────────┬────────────┐
   │ Project              │ Status     │
   ├──────────────────────┼────────────┤
   │ elopool              │ ✅ linked  │
   │ claude               │ ✅ linked  │
   │ my-app               │ ✅ linked  │
   │ broken-project       │ ⚠️ broken  │
   └──────────────────────┴────────────┘
   ```

## Idempotency Guarantees

- **Symlinks:** checked before creation — existing correct symlinks are never touched
- **Directories:** `mkdir -p` is inherently idempotent
- **Configs:** compared before copying — identical files are skipped
- **Re-runs:** produce zero side effects if nothing changed
- **add subcommand:** checks existing symlink before creating

## Error Handling

- Missing vault path: create it (not an error)
- Missing templates: warn and skip config deployment (not an error)
- No projects with `.planning/`: report and skip symlinks (not an error)
- Broken symlink found during verification: report with warning symbol
- Permission denied: report the specific path and stop
- `add` with nonexistent project: abort with clear error message

## Rules

- Always run all bash commands with quoted paths (the iCloud path has spaces)
- Never modify the projects themselves — only create symlinks in the vault
- Never copy `workspace.json` — it contains machine-specific window state
- Never remove existing symlinks unless they point to the wrong target
- The skill is instructions for Claude Code to follow, not a standalone shell script
