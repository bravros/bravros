---
name: commit
description: >
  Commit current changes following emoji+type conventions with code formatting.
  Use this skill whenever the user says "/commit", "commit this", "commit changes",
  "save my changes", or any request to create a git commit.
  Also triggers on "stage and commit", "commit with message", "make a commit",
  "let's commit", or "save this progress".
  Do NOT trigger when the user also mentions pushing — route to /ship instead.
  Runs pint formatting on PHP files before committing.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Commit: Stage and Commit Changes

Commit the current changes following our conventions. **Commit only — do not push to remote.**

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Process

## Step 1/3: Review Changes

```bash
echo "🔀 commit [1/3] Reviewing staged and unstaged changes"
```

1. Review changes: `git status` and `git diff`
2. Stage only necessary files (never `git add -A` blindly)
3. ⛔ NEVER stage `.env`, `.env.*`, `*-api-key`, `credentials.json`, or files containing API keys/tokens

## Step 2/3: Detect and Run Formatter

```bash
echo "🔀 commit [2/3] Running code formatter"
```

Read `~/.claude/skills/context/references/test-runners.md` (Formatter Detection section) and detect the project's code formatter:

| Language | Formatter | Detection |
|----------|-----------|-----------|
| PHP | Pint | `laravel/pint` in `composer.json` |
| JavaScript/TypeScript | Prettier or ESLint | `prettier` or `eslint` in `package.json` |
| Python | Black or Ruff | `[tool.black]` or `[tool.ruff]` in `pyproject.toml` |
| Ruby | RuboCop | `rubocop` in `Gemfile` |
| Go | gofmt (built-in) | `gofmt -w .` or `goimports -w .` |
| Rust | cargo fmt (built-in) | `cargo fmt` |

**If PHP files changed:** Run `vendor/bin/pint --dirty` (check only dirty files) or `vendor/bin/pint` (all files)
**If JS/TS files changed:** Run `npx prettier --write .` or `npx eslint --fix .`
**If Python files changed:** Run `black .` or `ruff format .`
**If Go files changed:** Run `gofmt -w .` or `goimports -w .`
**If Rust files changed:** Run `cargo fmt`
**If no formatter detected:** Skip formatting step and note: `[formatter] No formatter detected`

## Step 3/3: Write Message and Commit

```bash
echo "🔀 commit [3/3] Writing commit message and committing"
```

Write a concise 1-2 sentence commit message focusing on **why** the change was made, not just what changed. Commit with emoji format: `<emoji> <type>: <description>`

## Emoji Types

| Emoji | Type | Use |
|-------|------|-----|
| ✨ | feat | New features |
| 🐛 | fix | Bug fixes |
| 📚 | docs | Documentation |
| 💄 | style | Formatting |
| ♻️ | refactor | Restructuring |
| ⚡ | perf | Performance |
| 🧪 | test | Testing |
| 🔧 | build | Build changes |
| 🧹 | chore | Maintenance |
| 📋 | plan | Planning |
| 🔒 | security | Security |
| 🗃️ | migration | DB migrations |
| 📦 | deps | Dependencies |
| 🔀 | merge | Branch merges |

Present tense, lowercase, atomic commits. NEVER add AI signatures.

Use $ARGUMENTS as context for the commit message if provided.