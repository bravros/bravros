---
name: quick
description: Quick task execution without a full plan — just do it and commit.
---

# Quick: Fast Task Execution

Quick task execution without a full plan — just do it and commit.

> [!IMPORTANT]
> Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

## Overview

- **Auto-branch**: a `/scout` handoff (`debug: S-NNNN`) gets `fix/<slug>` off `origin/<staging>` automatically.
- **Branch safety**: product code is never edited on the staging branch or `main`/`master` — cut
  `fix/<slug>` from `origin/<staging>` first. A one-file trivial change is allowed on the staging
  branch only when the operator asked for it to land there; never on `main`/`master` (direct-main
  repos excepted).
- **Implement & Verify**: Minimal targeted changes with quick verification.
- **Commit & Next**: Use `/commit` and suggest next actions (`/pr`, done, etc.).
