# Workflow Sync: Update Project Workflow Files

Sync an existing project with the latest workflow setup — hooks, GitHub Action, and DB sync template.

## Model Requirement

**Sonnet** — this skill performs mechanical/scripted operations that don't require deep reasoning.

```
/start         → first-time project setup (no clobber)
/workflow-sync → update existing project with latest workflow files (overwrites)
```

## What Gets Updated

| File | Action |
|------|--------|
| `.bravros/hooks/commit-msg` | Overwrite with latest |
| `.bravros/hooks/pre-push` | Overwrite with latest |
| `.github/workflows/claude.yml` | Overwrite with latest |
| `sync-db.sh` | **Skip if exists** — may have diverged |
| `.db-sync.env.example` | **Skip if exists** — pairs with the script |
| `CLAUDE.md` | **Skip** — project-specific |
| `.gitignore` | Append missing entries only |

## Process

### 1. Check Prerequisites
Verify git repository and `.bravros/hooks/` exists.

### 2. Update Git Hooks
```bash
bravros init --skip-staging-branch --skip-workflows
```

### 3. Update GitHub Action
```bash
mkdir -p .github/workflows
cp ~/.bravros/templates/.github/workflows/claude.yml .github/workflows/claude.yml
```

### 4. sync-db.sh — do NOT overwrite
Copy it only when the project has **no** `sync-db.sh` yet. A project that already
has one may have evolved it (dev-box target, remote import, tuned dump flags),
and overwriting silently reverts that work — the change is invisible until the
next sync is slow or fails. To pull in template improvements deliberately, diff
them in:

```bash
diff ~/.bravros/templates/sync-db.sh sync-db.sh
```

### 5. Update .gitignore
Append missing entries only.

## Rules
- Always overwrite hook files
- Never overwrite CLAUDE.md
- Never auto-commit — let user review
- Never overwrite an existing sync-db.sh or .db-sync.env.example — copy only when absent, and diff to adopt template changes

Use $ARGUMENTS for any additional context.
