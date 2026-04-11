---
name: report-template
description: Frontmatter schema and conventions for investigation reports in .planning/reports/
---

# Report Template

## Frontmatter Schema

```yaml
---
id: "R-NNNN"
title: "investigation: short description"
type: investigation     # investigation | debug | incident | audit | analysis
status: open            # open | in-progress | resolved | escalated | wont-fix
severity: medium        # critical | high | medium | low | info
project: project-name
plan: null              # plan ID that fixed this (e.g., "0194")
backlog: null           # backlog ID if escalated (e.g., "B-0039")
pr: null                # PR number if fix was shipped
tags: []
affected_entities: []   # domain-specific, flexible per project
root_cause: null        # one-line summary of root cause (filled when resolved)
created: YYYY-MM-DDTHH:MM
resolved: null
author: skaisser
---
```

## Filename Convention

`R-NNNN-<type>-<short-slug>-<status>.md`

Examples:
- `R-0001-investigation-pedido-18391-open.md`
- `R-0002-debug-fatura-duplicada-complete.md`

## Lifecycle

- Created as `-open.md`
- Renamed to `-complete.md` when resolved
- `sdlc finish` handles the rename and updates cross-references via `updateWikilinks`

## Cross-References

Reports can reference plans and backlog items using Obsidian wikilinks:
- `[[0194-fix-refund-retorno-complete]]` — links to a plan
- `[[B-0039-feat-producer-ticket-open]]` — links to a backlog item

Plans and backlog can reference reports back the same way.
