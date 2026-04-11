---
name: firecrawl-cli-installation
description: |
  Install the official Firecrawl CLI and handle authentication.
  Package: https://www.npmjs.com/package/firecrawl-cli
  Source: https://github.com/firecrawl/cli
  Docs: https://docs.firecrawl.dev/sdks/cli
---

# Firecrawl CLI Installation

## Quick Setup (Recommended)

```bash
npx -y firecrawl-cli -y
```

This installs `firecrawl-cli` globally, authenticates via browser, and installs all skills.

Skills are installed globally across all detected coding editors by default.

To install skills manually:

```bash
firecrawl setup skills
```

## Manual Install

```bash
npm install -g firecrawl-cli@1.8.0
```

## Verify

```bash
firecrawl --status
```

## Authentication

**Primary method (1Password):** API key is stored in 1Password as **"FireCrawl Api"** (field: `credencial`). Set it before using firecrawl:

```bash
export FIRECRAWL_API_KEY=$(op read "op://HomeLab/FireCrawl Api/credencial" 2>/dev/null)
```

This works instantly on any machine with `op` CLI — no login step needed.

**Fallback methods** (if 1Password is not available):

1. **Login with browser** - Run `firecrawl login --browser` (opens browser for OAuth)
2. **Enter API key manually** - Run `firecrawl login --api-key "<key>"` with a key from firecrawl.dev

### Command not found

If `firecrawl` is not found after installation:

1. Ensure npm global bin is in PATH
2. Try: `npx firecrawl-cli@1.8.0 --version`
3. Reinstall: `npm install -g firecrawl-cli@1.8.0`
