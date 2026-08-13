---
name: backlog
core: true
description: Capture, list, and promote pre-planning ideas. Use `/backlog` to add, view, promote, complete, or drop ideas before planning.
---

# backlog

INTENT: a parking lot for ideas — lightweight to capture, structured enough to evaluate
later. The backlog never implements; promotion hands off to `/plan`.

> [!IMPORTANT]
> Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/backlog/references/briefing.md) on demand for detailed context and instructions.

## Key Rules

1. **Events Model**: Item files (`.planning/backlog/B-NNNN-<slug>.md`) are identity-only and never renamed. State is derived from `.planning/events.jsonl`.
2. **ID Allocation**: `BID=$(bravros nextid reserve backlog)`
3. **Write Safety**: All write flows must execute from the base branch at `$BACKLOG_ROOT`, committed, and pushed immediately to prevent ID collisions.

## Command Summary

- `/backlog` — list active backlog items
- `/backlog <number>` — view details of a specific item
- `/backlog add <text>` — capture a new idea
- `/backlog promote <number|N-M>` — hand off idea to `/plan`
- `/backlog done|drop <number>` — complete or cancel an item
- `/backlog pending group [auto]` — cluster active items into plan-sized groups
