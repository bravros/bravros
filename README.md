# Bravros

**SDLC pipeline for Claude Code** — plan, build, test, deploy with confidence.

Bravros is a commercial CLI tool and Claude Code plugin that provides a complete software development lifecycle pipeline. It includes 50+ slash commands (skills), a Go CLI binary for audit enforcement, and pre-configured templates for GitHub Actions, git hooks, and project setup.

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/bravros/bravros/main/install.sh | bash
```

> Requires repo access. Contact your admin for an invitation.

## What you get

- **`bravros` CLI** — audit hooks, auto-update, status line, plan management
- **50+ skills** — `/plan`, `/auto-pr`, `/finish`, `/debug`, `/test`, and more
- **Git hooks** — commit message enforcement, pre-push checks
- **GitHub Actions** — CI/CD, automated PR review via @claude
- **MCP servers** — Context7 + Sequential Thinking auto-registered
- **Auto-update** — checks for new versions on every session start

## Quick start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/bravros/bravros/main/install.sh | bash

# Verify
bravros version
bravros skills list

# Start a project
cd ~/your-project
/start
```

## Updating

The CLI auto-checks for updates on every Claude Code session. To update manually:

```bash
bravros update --force
```

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/bravros/bravros/main/install.sh | bash -s -- --uninstall
```

## License

Proprietary — see [LICENSE](LICENSE).
