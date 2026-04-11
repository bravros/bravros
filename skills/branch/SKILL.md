---
name: branch
description: >
  Create a new feature branch following type/kebab-case conventions.
  Use this skill whenever the user says "/branch", "create branch", "new branch",
  "make a branch", or any request to create a git branch.
  Also triggers on "checkout new branch", "start a branch", "feature branch",
  "I need a new branch", or "branch for [something]".
  Uses type/description format: feat/feature-name, fix/bug-name, etc.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Branch: Create Feature Branch

Create a new feature branch following our conventions.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Format
```
<type>/<description>
```

## Types
- feat: New features
- fix: Bug fixes
- docs: Documentation
- style: Formatting
- refactor: Restructuring
- perf: Performance
- test: Testing
- build: Build changes
- chore: Maintenance

## Rules
- Use kebab-case for description
- Keep it short and descriptive
- Match the commit type you'll use

## Process

1. Create the branch using the CLI:
   ```bash
   ~/.claude/bin/bravros branch create <type>/<branch-name>
   ```
   Returns JSON `{"branch": "feat/x", "base": "main", "created": true}`. The CLI handles base branch detection, pulling latest, and branch creation in one step.

Do NOT commit or push anything — branch creation only.

Use $ARGUMENTS as the branch name/description.
