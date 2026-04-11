---
name: obsidian-migrate
description: >
  Normalize .planning/ files to Obsidian-compatible YAML frontmatter (v2 format).
  Use this skill whenever the user says "/obsidian-migrate", "migrate planning files",
  "normalize frontmatter", "fix planning files", "obsidian compat", or any request to
  convert .planning/ files to the canonical v2 YAML frontmatter format.
  Also triggers on "make planning files obsidian compatible", "fix dates in planning",
  "normalize planning yaml", or any request to standardize .planning/ file formats.
  This skill is idempotent — running it multiple times on the same project is safe.
---

# Obsidian Migrate: Normalize `.planning/` Files

Scan and normalize all `.planning/` markdown files in the current project to the canonical v2 YAML frontmatter format, making them fully compatible with Obsidian Properties, Dataview, and Graph View.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Critical Rules

1. **Never lose data.** Field values are preserved during normalization — only field names and formats change.
2. **Idempotent.** Files already in canonical v2 format are skipped and reported as "already migrated".
3. **Use python3.** Write and execute a Python script for the actual migration logic.
4. **Dry-run first.** Always show the summary report BEFORE writing changes. Ask user to confirm.
5. **Commit per project.** After migration, commit with: `🧹 chore: migrate .planning/ to Obsidian-compatible frontmatter`

## Canonical v2 Frontmatter Format

```yaml
---
id: "0042"
title: "feat: Short Description"
type: feat
status: todo
project: my-project
branch: feat/short-description
base: homolog
tags:
  - payment
  - webhook
backlog: null
created: 2026-03-23T14:00
completed: null
pr: null
session: null
---
```

### Field Type Rules (Obsidian Properties)

| Field | Type | Format |
|-------|------|--------|
| `id` | Text | Double-quoted string, e.g. `"0042"` |
| `title` | Text | Double-quoted string |
| `type` | Text | Unquoted |
| `status` | Text | Unquoted |
| `project` | Text | Unquoted |
| `branch` | Text | Unquoted |
| `base` | Text | Unquoted |
| `tags` | List | Multiline YAML list (one `- tag` per line) |
| `backlog` | Text | Double-quoted string or `null` |
| `created` | Date & time | `YYYY-MM-DDTHH:mm` (no seconds, no quotes) |
| `completed` | Date & time | `YYYY-MM-DDTHH:mm` or `null` |
| `pr` | Text | URL string or `null` |
| `session` | Text | UUID string or `null` |

### Optional Fields (preserved if present)

| Field | Type | Notes |
|-------|------|-------|
| `strategy` | Text | Added by `/plan-review` |
| `reviews` | List | Added by `/plan-review` |
| `depends` | Text | Cross-plan dependency |
| `plan` | Text | Plan ID (in backlog items) |
| `priority` | Text | Backlog priority |
| `effort` | Text | Backlog effort estimate |

## Step 1/5: Detect Project and Scan Files

```bash
echo "📓 obsidian-migrate [1/5] detecting project and scanning files"
```

1. Detect project name from `basename $(pwd)`
2. Check `.planning/` directory exists — abort if not
3. Find all `.md` files in `.planning/` (root), `.planning/backlog/`, and `.planning/backlog/archive/`
4. Report total file count

## Step 2/5: Write and Run Migration Script

```bash
echo "📓 obsidian-migrate [2/5] running migration analysis"
```

Write a Python 3 script to `/tmp/obsidian_migrate.py` and execute it. The script must:

### 2a. File Format Classification

For each `.md` file, classify into one of four categories:

1. **No frontmatter** — file has no `---` delimited YAML block at the top
2. **Blockquote format** — file uses `> **Status:** value` pattern (v0 format)
3. **Old YAML (v1)** — has YAML frontmatter but contains deprecated fields
4. **Canonical v2** — already in the correct format (skip)

Detection logic:
```python
def classify_file(content):
    """Classify a .planning/ file's frontmatter format."""
    lines = content.split('\n')

    # Check for YAML frontmatter
    if not lines[0].strip() == '---':
        # Check for blockquote format
        if any(line.startswith('> **') for line in lines[:20]):
            return 'blockquote'
        return 'no_frontmatter'

    # Find closing ---
    end_idx = None
    for i, line in enumerate(lines[1:], 1):
        if line.strip() == '---':
            end_idx = i
            break

    if end_idx is None:
        return 'no_frontmatter'

    # Parse YAML block
    yaml_text = '\n'.join(lines[1:end_idx])

    # Check for deprecated v1 fields
    v1_fields = [
        'plan_file:', 'base_branch:', 'sessions:',
        'phases_total:', 'phases_done:', 'tasks_total:', 'tasks_done:',
        'created_on:', 'total_tasks:', 'completed_tasks:', 'name: plan'
    ]

    for field in v1_fields:
        if field in yaml_text:
            return 'old_yaml'

    # Check for date format issues (DD/MM/YYYY)
    import re
    if re.search(r'created:.*\d{2}/\d{2}/\d{4}', yaml_text):
        return 'old_yaml'

    # Check for single-quoted or unquoted IDs
    id_match = re.search(r"^id:\s*'", yaml_text, re.MULTILINE)
    if id_match:
        return 'old_yaml'
    id_match = re.search(r"^id:\s*(\d+)\s*$", yaml_text, re.MULTILINE)
    if id_match:
        return 'old_yaml'

    # Check for inline tag arrays
    if re.search(r'^tags:\s*\[', yaml_text, re.MULTILINE):
        return 'old_yaml'

    return 'v2_canonical'
```

### 2b. Format Conversion

**No frontmatter → v2:**
```python
def convert_no_frontmatter(content, filepath, project):
    """Generate v2 frontmatter from a file with none."""
    lines = content.split('\n')

    # Extract title from first # heading
    title = None
    for line in lines:
        if line.startswith('# '):
            title = line[2:].strip()
            break

    # Infer type from filename
    filename = os.path.basename(filepath)
    type_map = {
        'feat': 'feat', 'fix': 'fix', 'refactor': 'refactor',
        'chore': 'chore', 'hotfix': 'hotfix', 'docs': 'docs',
        'test': 'test', 'perf': 'perf', 'style': 'style',
        'build': 'build', 'debug': 'chore', 'investigation': 'chore'
    }
    inferred_type = 'chore'
    for key, val in type_map.items():
        if key in filename.lower():
            inferred_type = val
            break

    # Extract ID from filename (NNNN- prefix)
    id_match = re.match(r'^(\d{4})-', filename)
    file_id = f'"{id_match.group(1)}"' if id_match else '"0000"'

    # Infer status from filename suffix
    status = 'completed'
    if '-todo' in filename.lower():
        status = 'todo'
    elif '-in-progress' in filename.lower() or '-inprogress' in filename.lower():
        status = 'in-progress'

    # Determine if it's a backlog file
    is_backlog = '/backlog/' in filepath

    if is_backlog:
        frontmatter = f'''---
id: {file_id}
title: "{title or filename}"
type: idea
status: captured
project: {project}
priority: medium
effort: null
tags:
  - uncategorized
depends: null
created: null
plan: null
---'''
    else:
        frontmatter = f'''---
id: {file_id}
title: "{inferred_type}: {title or filename}"
type: {inferred_type}
status: {status}
project: {project}
branch: null
base: null
tags:
  - uncategorized
backlog: null
created: null
completed: null
pr: null
session: null
---'''

    return frontmatter + '\n\n' + content
```

**Blockquote format → v2:**
```python
def convert_blockquote(content, filepath, project):
    """Parse blockquote metadata into v2 YAML frontmatter."""
    lines = content.split('\n')
    meta = {}

    # Parse > **Key:** Value lines
    for line in lines:
        match = re.match(r'>\s*\*\*(\w[\w\s]*):\*\*\s*(.*)', line)
        if match:
            key = match.group(1).strip().lower().replace(' ', '_')
            value = match.group(2).strip()
            meta[key] = value

    # Remove blockquote lines from body
    body_lines = [l for l in lines if not re.match(r'>\s*\*\*\w', l)]
    body = '\n'.join(body_lines).strip()

    # Map to v2 fields
    # ... (use meta dict to populate frontmatter)
    # Then return frontmatter + body
```

**Old YAML → v2 (field normalization):**
```python
def normalize_yaml(yaml_dict, filepath):
    """Normalize v1 YAML fields to v2 canonical format."""
    changes = []

    # Field renames
    if 'base_branch' in yaml_dict:
        yaml_dict['base'] = yaml_dict.pop('base_branch')
        changes.append('base_branch → base')

    if 'created_on' in yaml_dict:
        yaml_dict['created'] = yaml_dict.pop('created_on')
        changes.append('created_on → created')

    # Field drops
    drop_fields = [
        'plan_file', 'phases_total', 'phases_done',
        'tasks_total', 'tasks_done', 'total_tasks',
        'completed_tasks'
    ]
    for field in drop_fields:
        if field in yaml_dict:
            yaml_dict.pop(field)
            changes.append(f'dropped {field}')

    # Drop name: plan
    if yaml_dict.get('name') == 'plan':
        yaml_dict.pop('name')
        changes.append('dropped name: plan')

    # sessions (array) → session (single)
    if 'sessions' in yaml_dict:
        sessions = yaml_dict.pop('sessions')
        if isinstance(sessions, list) and sessions:
            last = sessions[-1]
            if isinstance(last, dict):
                yaml_dict['session'] = last.get('id') or last.get('session_id')
            else:
                yaml_dict['session'] = str(last)
        else:
            yaml_dict['session'] = None
        changes.append('sessions[] → session')

    return yaml_dict, changes
```

### 2c. Date Conversion

```python
def normalize_date(value):
    """Convert any date format to YYYY-MM-DDTHH:mm."""
    if value is None or value == 'null' or value == '':
        return None

    value = str(value).strip().strip('"').strip("'")

    # DD/MM/YYYY HH:MM
    match = re.match(r'(\d{2})/(\d{2})/(\d{4})\s+(\d{2}):(\d{2})', value)
    if match:
        d, m, y, H, M = match.groups()
        return f'{y}-{m}-{d}T{H}:{M}'

    # DD/MM/YYYY (no time)
    match = re.match(r'(\d{2})/(\d{2})/(\d{4})$', value)
    if match:
        d, m, y = match.groups()
        return f'{y}-{m}-{d}T00:00'

    # YYYY-MM-DDTHH:mm:ss (strip seconds)
    match = re.match(r'(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}):\d{2}', value)
    if match:
        return f'{match.group(1)}T{match.group(2)}'

    # YYYY-MM-DD HH:MM (add T)
    match = re.match(r'(\d{4}-\d{2}-\d{2})\s+(\d{2}:\d{2})', value)
    if match:
        return f'{match.group(1)}T{match.group(2)}'

    # YYYY-MM-DDTHH:mm (already canonical)
    match = re.match(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$', value)
    if match:
        return value

    # YYYY-MM-DD (date only, add T00:00)
    match = re.match(r'(\d{4}-\d{2}-\d{2})$', value)
    if match:
        return f'{value}T00:00'

    # Could not parse — return as-is and flag
    return value
```

### 2d. ID Quoting Normalization

```python
def normalize_id(value):
    """Ensure ID is double-quoted string."""
    if value is None:
        return '"0000"'
    # Remove existing quotes
    val = str(value).strip().strip('"').strip("'")
    # Pad to 4 digits if numeric
    if val.isdigit():
        val = val.zfill(4)
    return f'"{val}"'
```

### 2e. Tag Array Normalization

```python
def normalize_tags(value):
    """Convert inline [tag1, tag2] to list."""
    if isinstance(value, list):
        return value
    if isinstance(value, str):
        # Parse inline array: [tag1, tag2]
        match = re.match(r'\[(.*)\]', value.strip())
        if match:
            return [t.strip().strip('"').strip("'") for t in match.group(1).split(',') if t.strip()]
        return [value]
    return []
```

### 2f. References Section Generator

After normalizing frontmatter, scan for cross-reference fields and add a `## References` section to the body if it doesn't already exist:

```python
def generate_references_section(yaml_dict, body):
    """Add ## References with wikilinks for cross-referenced items."""
    if '## References' in body:
        return body  # Already has references section

    refs = []

    # backlog → link to backlog item
    if yaml_dict.get('backlog') and yaml_dict['backlog'] != 'null':
        backlog_id = str(yaml_dict['backlog']).strip('"').strip("'")
        refs.append(f'- Backlog: [[{backlog_id}]]')

    # plan → link to plan (in backlog items)
    if yaml_dict.get('plan') and yaml_dict['plan'] != 'null':
        plan_id = str(yaml_dict['plan']).strip('"').strip("'")
        refs.append(f'- Plan: [[{plan_id}]]')

    # depends → link to dependency
    if yaml_dict.get('depends') and yaml_dict['depends'] != 'null':
        depends_val = str(yaml_dict['depends']).strip('"').strip("'")
        refs.append(f'- Depends on: [[{depends_val}]]')

    if not refs:
        return body

    refs_section = '\n## References\n\n' + '\n'.join(refs) + '\n'
    return body.rstrip() + '\n' + refs_section
```

## Step 3/5: Show Summary Report

```bash
echo "📓 obsidian-migrate [3/5] migration summary"
```

The Python script must output a summary table:

```
╔══════════════════════════════════════════════════════════════════╗
║  Obsidian Migration Report — {project}                         ║
╠══════════════════════════════════════════════════════════════════╣
║  Total files scanned:    {N}                                   ║
║  Already v2 (skipped):   {N}                                   ║
║  No frontmatter → v2:    {N}                                   ║
║  Blockquote → v2:        {N}                                   ║
║  Old YAML → v2:          {N}                                   ║
║  Failed (manual review):  {N}                                  ║
╚══════════════════════════════════════════════════════════════════╝

Per-file changes:
┌─────────────────────────────────┬──────────┬─────────────────────────┐
│ File                            │ Format   │ Changes                 │
├─────────────────────────────────┼──────────┼─────────────────────────┤
│ 0001-feat-auth-todo.md          │ old_yaml │ base_branch→base,       │
│                                 │          │ date converted,         │
│                                 │          │ tags normalized         │
│ 0002-fix-payment-completed.md   │ no_fm    │ frontmatter generated   │
│ 0003-refactor-api-todo.md       │ v2       │ (already migrated)      │
│ debug-stuck-orders.md           │ no_fm    │ ⚠ no ID — manual review │
└─────────────────────────────────┴──────────┴─────────────────────────┘
```

Files that cannot be auto-migrated (no ID extractable, ambiguous format) are flagged with a warning symbol and listed separately for manual review.

## Step 4/5: Apply Changes

```bash
echo "📓 obsidian-migrate [4/5] applying changes"
```

**Use `AskUserQuestion` to confirm before writing.**

- Question: "Migration report ready. Apply changes to {N} files?"
- Option 1: "Apply all changes" — write all migrated files
- Option 2: "Show me a diff first" — show before/after for each file
- Option 3: "Cancel" — abort without changes

After applying:
1. Write each modified file back to disk
2. Show final count: `✅ Migrated {N} files in {project}/.planning/`

## Step 5/5: Commit (if requested)

If the user wants to commit after migration:

```bash
git add .planning/
git commit -m "🧹 chore: migrate .planning/ to Obsidian-compatible frontmatter"
```

## Complete Python Script Template

The skill should write the full script to `/tmp/obsidian_migrate.py`. The script:

1. Takes project directory as argument: `python3 /tmp/obsidian_migrate.py /path/to/project`
2. Scans `.planning/` recursively for `.md` files
3. Classifies each file
4. Applies conversions (in memory, not to disk)
5. Outputs JSON with the migration plan: `{ "files": [...], "summary": {...} }`
6. With `--apply` flag, writes changes to disk

Key implementation notes:
- Use `import re, os, sys, json, glob` — stdlib only, no pip dependencies
- Parse YAML manually with regex (avoid requiring PyYAML) — frontmatter is simple enough
- Handle edge cases: empty files, files with only frontmatter, files with malformed YAML
- Preserve the body content exactly as-is (only modify frontmatter and add References section)
- When writing frontmatter, maintain the canonical field order from the template above
- Handle the `reviews:` field specially — it's a list of strings that may contain colons

## Idempotency

The script ensures idempotency by:
1. Classifying files first — v2 canonical files are never modified
2. Tracking changes — if no changes would be made, the file is skipped
3. Comparing output to input — if the normalized version equals the original, skip

## Error Handling

- Files that fail parsing: log the error and continue, add to "manual review" list
- Files with no extractable ID: generate as `"0000"` and flag for manual review
- Files with ambiguous date formats: flag for manual review
- Empty files: skip entirely

## Rules

- Always run from the project root directory (where `.planning/` exists)
- Never modify files outside `.planning/`
- Never delete files — only modify in-place
- Preserve git history — this is an in-place normalization, not a file move
- The `## References` section is appended at the very end of the file body
- Cross-reference wikilinks use the ID only (e.g., `[[0005]]`), not the full filename — Obsidian resolves these automatically
