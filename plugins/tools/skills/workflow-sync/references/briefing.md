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
| `sync-db.sh` | Overwrite if exists |
| `.db-sync.env.example` | Overwrite if exists |
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

### 4. Update sync-db.sh (if exists)
Only update if project already has it.

### 5. Update .gitignore
Append missing entries only.

## Rules
- Always overwrite hook files
- Never overwrite CLAUDE.md
- Never auto-commit — let user review
- Only update sync-db.sh if project already has it

Use $ARGUMENTS for any additional context.
