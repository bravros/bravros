# Obsidian Config Templates

Canonical Obsidian configuration files captured from the primary vault. These are
deployed to a new vault's `.obsidian/` directory by the `/obsidian-setup` skill.

> **Note:** These are macOS-specific configs. Obsidian stores plugin and vault data
> at paths that differ between macOS, Windows, and Linux.

## Files

| File | Purpose |
|------|---------|
| `app.json` | Core Obsidian app settings (editor behavior, line width, spellcheck, etc.) |
| `appearance.json` | Theme, font, and UI density settings |
| `community-plugins.json` | List of installed community plugins (safe mode off + enabled plugins) |
| `core-plugins.json` | Which built-in Obsidian plugins are active (backlinks, templates, etc.) |
| `graph.json` | Graph view display settings (colors, forces, filters) |
| `hotkeys.json` | Custom keyboard shortcut overrides |

## Plugin Configs (`plugins/`)

Each subdirectory contains the `data.json` for that plugin — the user-configured
settings persisted by the plugin itself.

| Plugin dir | Plugin |
|------------|--------|
| `obsidian-linter/` | Obsidian Linter — auto-format rules on save |
| `obsidian-icon-folder/` | Icon Folder — custom icons per folder/file |

> **Note:** Dataview and Kanban do not generate a `data.json` until their settings
> are changed from defaults, so they are not included here.

## How to Update

If you change settings in Obsidian and want to capture them:

1. Make your changes in the Obsidian app.
2. Run `/sync` from Claude Code — it copies `~/.claude/templates/` to this repo
   automatically via `cp -rf ~/.claude/templates/. "$SYNC_DIR/templates/"`.
3. Alternatively, manually copy changed files from
   `~/Library/Mobile Documents/com~apple~CloudDocs/Sync/obsidian/.obsidian/`
   to `templates/obsidian/` and commit.

Do NOT copy `workspace.json` — it contains machine-specific open-file state and
should not be shared across machines.
