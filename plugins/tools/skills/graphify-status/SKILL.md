---
name: graphify-status
description: Report knowledge-graph label coverage across every graphify-enabled project on this machine — communities, labels, and how many nodes still render as "Community NN". Use on `/graphify-status`, or when asked which graphs are stale, unlabelled, or degraded.
trigger: /graphify-status
---

# /graphify-status

> Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

One table, every graphify project on the machine, answering a single question: **does `graphify query` return real names, or is it still handing back `Community 417`?**

## Quick Start

Run the scanner:

```bash
uv run ~/.bravros/skills/graphify-status/scripts/graphify-status.py
```

Print the table verbatim. Do not re-measure by hand or re-format it — the script is the source of truth.

Options:
- `--json`: machine-readable rows instead of table
- `--depth N`: walk depth per root (default 3)
- `--no-prompt`: skip auto-generating labelling prompts
- `<root> ...`: scan specific roots instead of the defaults (`~/Code`, plus `~/Sites` if present)

## Key Rules

- **Degraded graphs**: If a run reports degraded coverage, follow the prompt verdict in `/tmp/graphify-label/<project>-relabel-prompt-N.md`.
- **Merge results**: Merge completed label JSONs using `merge-missing-labels.py` (additive, never overwrites without `--force`).
- **Never use `collate-labels.py`** for partial relabels.
