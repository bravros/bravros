---
name: triage-sweep
core: true
description: Read-only drain of a stale issue + backlog queue — dedup, classify each item vs LIVE code (already-done / partial / superseded / no-longer-needed / open / human-only), adversarially verify every close, then apply closes/cancels serially. Use on /triage-sweep.
---

# Triage Sweep — dedup → classify-vs-code → adversarial-verify → serial apply

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

## Quick Summary & Non-Negotiable Guards

1. **Read-only fan-out, serial apply.** The workflow mutates NOTHING; every `gh` close and every ledger event is applied **serially** afterward.
2. **Evidence-gated close.** An item auto-closes ONLY when classified `already-done`/`solved-differently` with a concrete `artifact_ref` AND an independent adversarial verifier confirms it.
3. **Project guard seam.** Pass project rules via `args.guards`.
4. **Never close in-flight or defective-ledger items.**

## Workflow Execution Steps

1. **Step 0 — Preflight + materialize:**
   ```bash
   STAGING=$(grep -E '^staging_branch:' .bravros.yml 2>/dev/null | awk '{print $2}'); STAGING=${STAGING:-homolog}
   mkdir -p .agent_config/workflows && cp -f ~/.agent_config/skills/triage-sweep/scripts/triage-sweep.js .agent_config/workflows/triage-sweep.js
   ```
2. **Step 1 — Triage (parallel, read-only):** Run `triage-sweep` workflow across code, worktrees, open PRs, and `.planning/` plan folders.
3. **Step 2 — Apply (SERIAL):** Append event to `.planning/events.jsonl` or run `gh issue close`.
4. **Step 3 — Ledger + close out:** Write `.planning/sweep-ledger.md` and announce completion via `bravros ha say`.
