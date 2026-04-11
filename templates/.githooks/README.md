# Git Hooks

This directory contains shared git hooks for the project.

## Setup

Git is configured to use this directory for hooks. New clones need to run:

```bash
git config core.hooksPath .githooks
```

## Hooks

### commit-msg

Validates commit messages follow our conventions.

**Format:** `<emoji> <type>: <description>`

| Emoji | Type | Description |
|-------|------|-------------|
| ✨ | feat | New features |
| 🐛 | fix | Bug fixes |
| 📚 | docs | Documentation |
| 💄 | style | Formatting |
| ♻️ | refactor | Restructuring |
| ⚡ | perf | Performance |
| 🧪 | test | Testing |
| 🔧 | build | Build changes |
| 🧹 | chore | Maintenance |
| 📋 | plan | Planning updates |
| 🔒 | security | Security fixes |
| 🗃️ | migration | Database migrations |
| 📦 | deps | Dependency updates |
| 🚀 | deploy | Deployment/CI changes |
| 🔥 | remove | Removing code/features |
| 🩹 | hotfix | Urgent fixes |
| 🔀 | merge | Branch merges |

**Rules enforced:**
- No AI signatures (Co-Authored-By: Claude, etc.)
- Must start with valid emoji + type
- Must have description after colon
- Description should be lowercase (warning only)

**Examples:**
```
✨ feat: add user authentication
🐛 fix: resolve null pointer in checkout
🧪 test: add coverage for payment flow
📋 plan: add 0116 tiktok shop integration
```
