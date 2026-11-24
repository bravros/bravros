---
name: claudemd
description: Use on /claudemd to generate or audit CLAUDE.md files with stack auto-detection and Context7 docs. Not the built-in /context (token usage). Also for refreshing project instructions.
---

# claudemd — generate & audit CLAUDE.md files

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: detect the stack, then generate/audit a tree of **lean** CLAUDE.md files via parallel `claudemd-author` workers — one per directory cluster. `$ARGUMENTS` = a directory path or flag.

HARD CONSTRAINTS:
- **Never overwrite an existing CLAUDE.md without `--force`** — but always audit it for staleness and present findings for approval.
- **The leader never writes CLAUDE.md files directly** — all authoring goes through the `context-authors` workflow's parallel workers; that partitioning is what makes concurrent writes safe.
- **No auto-commit** — the user reviews generated files.
- **Emitted files obey the survival rule**: every line must be something only this repo knows (non-obvious convention, trap, tool gotcha, authority boundary). Anything the model would infer from the code stays out — the worker prompt in `scripts/context-authors.js` enforces this. Root CLAUDE.md ≤60 lines: a MAP, not an encyclopedia.

## Quick Summary

1. Detect stack → update `<!-- BRAVROS:CONTEXT:STACK START/END -->` in root `CLAUDE.md`.
2. Enrich via Context7 (optional/if present).
3. Scan for existing/warranted `CLAUDE.md` files or community clusters.
4. Dispatch `context-authors` parallel workers.
5. Present audit findings/staleness for user approval via user input.
6. Report summary of actions taken.

## Flags

`--force`/`-f` regenerate all · `--dry-run`/`-d` preview · `--root`/`-r` root file only · `--audit`/`-a` audit only · `--no-context7`
