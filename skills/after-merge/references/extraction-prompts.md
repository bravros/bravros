# Extraction sub-agent — prompt, schema, idempotency pattern

## Sub-agent prompt core

```
You are extracting deploy context for PR #<N> in the <PROJECT> repository.
Working directory: <REPO_ROOT>

Pull and parse three sources, then return structured output.

1. PR body — gh pr view <N> --json body,title,number,mergedAt,headRefName
   Sections: Test Plan / Deployment notes / Post-deploy / References; any mention of
   migration, backfill, seed, cache:clear, queue:restart, artisan, "after deploy".
2. Linked plan — grep -r "#<N>" .planning/ --include="*.md" -l (also `pr: <N>`); read it;
   flag data-touching tasks and schema changes.
3. Review thread — gh pr view <N> --json reviews --jq '.reviews[] | .body'
   Reviewer observations that did NOT block merge ("Note:", "Consider:", "Watch for:",
   edge cases, performance flags) → watchpoints.

Return EXACTLY the JSON shape below — no prose, no markdown. If validation fails, fix
using the validator error verbatim and re-emit only the JSON.

{
  "pr": <number>,
  "title": "<PR title>",
  "merged_at": "<ISO date>",
  "post_deploy_actions": [
    {
      "name": "<short action name>",
      "description": "<what it does>",
      "source": "pr_body | plan_file | review",
      "has_data_mutation": <true|false>,
      "idempotency_check": "<SQL or bash check, or null if not documented>",
      "dry_run_command": "<command, or null>",
      "wet_run_command": "<command, or null>",
      "rollback_command": "<inverse command, or null>"
    }
  ],
  "watchpoints": ["<reviewer observation>"],
  "migrations": ["<migration file or description>"],
  "monitoring_notes": ["<specific thing to monitor>"],
  "has_data_backfill": <true|false>,
  "has_post_deploy_actions": <true|false>
}

One-shot example (correct minimal return):
{"pr": 42, "title": "Add API rate limiting", "merged_at": "2026-01-10", "post_deploy_actions": [], "watchpoints": ["Reviewer flagged possible N+1 on the new middleware query — watch p95 latency after deploy"], "migrations": [], "monitoring_notes": ["p95 latency on /api routes"], "has_data_backfill": false, "has_post_deploy_actions": false}
```

## The idempotency pattern (gold standard: freight backfill, PR #703 — recovered R$ 33.950,54)

Every data-modifying post-deploy action gets exactly four phases, in order:

- **A — Idempotency check first:** `SELECT COUNT(*) … WHERE <target> AND <NOT-already-processed>` (e.g. `valor_frete IS NULL`). N>0 on first run; N=0 means it already ran — skip.
- **B — Dry-run:** same logic with `LIMIT 10`, showing the computed values; review manually, abort if anything looks wrong.
- **C — Spot-check:** 3 rows verified by hand (tinker/psql) against the source of truth.
- **D — Wet-run:** full statement only after A–C pass; then re-run the check — it must now return 0.

**Generating a candidate guard** when none is documented: identify the target table, then
what "already processed" looks like (a column that gets SET / a `processed_at` timestamp /
a terminal status / a boolean flag), write the `AND NOT <processed>` count query, and flag
it `⚠️ CANDIDATE — verify the 'already processed' condition before wet-run`. A plain
`UPDATE … WHERE condition` or `INSERT … SELECT` with no such guard re-runs destructively —
always add the guard or `ON CONFLICT/ON DUPLICATE KEY`.

## Multi-PR aggregation

- Pre-deploy checks and watchpoints: collect all, deduplicate; keep watchpoints grouped by PR for traceability.
- Deploy steps: ONE shared stack-specific sequence — all PRs deploy together.
- Post-deploy actions and rollback: keep per-PR grouping; order actions by dependency (PR B using a column PR A adds runs after A — flag it `⚠️ Run AFTER PR #A's actions complete`), otherwise ascending PR number.
