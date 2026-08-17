---
name: worktree
description: Create, destroy, list or sync git worktrees for any project — Herd link+TLS, .env isolation, optional DB clone. Use on `/worktree`.
---

# /worktree — parallel-worktree manager

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

Parallel checkouts without colliding `.test` domains, Redis keys, sessions or queue jobs.
Laravel repos additionally get a Herd URL, isolated `.env`, and optionally a cloned DB.
**Scripts do the real work** — dispatch the right one from the repo/workspace root, relay output.

```
/worktree create [<app>] [<id>] [--branch=<name>] [--clone-db] [--live-dump] [--shared-db] [--fresh]
/worktree destroy <name> [--dry-run] [--force] [--yes] [--merged-into=<ref>] [--fresh]
/worktree list [--app=<repo>] [--fresh]
/worktree sync <name> [--onto=<ref>] [--merge] [--dry-run] [--fresh]
```

## Operator conventions

- **Derive the id yourself**: condense feature description to ≤12-char slug, report name, URL and **path**.
- **Shared parent DB is default — never ask.** `--clone-db` only when explicitly asked or running migrations.
- **Parent checkout is never switched.** `create` branches off `origin/<base>`.

## Commands

- **create** — `bash <skill>/scripts/create.sh [<app>] [<id>] [flags]`, stream stdout.
- **destroy** — `--dry-run` first, confirm via `ask_question` unless authorized, then `--yes`. Relay refusals verbatim.
- **list** — `list.sh [--app=<repo>]`. Clean unmanaged with `bravros worktree cleanup <path> --force`.
- **sync** — `sync.sh <name> [--onto=<ref>]`. Rebases (`--merge`), never pushes.
