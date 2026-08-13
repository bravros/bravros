# Certification Cookbook

**Code reading produces hypotheses; only runtime observation produces proof.**

## The three proofs

A hypothesis is certified when you hold at least one, with pasteable output (aim for two when the bug is subtle or the blast radius is large):

1. **Reproduce + state match** — trigger the failure, inspect state at the failure point, confirm it matches the prediction exactly.
2. **Counterfactual** — exercise the suspect logic in isolation; the wrong result appears for the predicted reason and disappears once the predicted condition is removed.
3. **Unbroken evidence chain** — every link symptom → cause verified against real source **and** real runtime data, no gaps.

## Read-only discipline

- **Allowed:** `SELECT` queries, value/object inspection, reading config / logs / routes / schema, running *existing* tests, dry-run reproductions, non-persisting REPL expressions.
- **Forbidden:** writing/updating/deleting data, migrations, `cache:clear` on shared environments, dispatching jobs with side effects.

If a proof seems to require mutating state, design a narrower read-only probe — or describe a reproduction for the user to run themselves and certify from their output.

## Laravel — `php artisan` one-shots (default; Boost MCP optional)

No MCP schema overhead, same proof power:

| Need | artisan probe |
|------|---------------|
| Execute the suspect expression in the live app context — the single most powerful proof tool | `php artisan tinker --execute='<expr>;'` |
| Read-only row-state checks | `php artisan tinker --execute='var_export(DB::select("SELECT …"));'` |
| Column types, nullability, indexes, FKs the hypothesis depends on | `php artisan db:table <table>` (or `db:show`) |
| Which controller + middleware serves the failing endpoint | `php artisan route:list --path=<fragment> --json` |
| A config/env value the behavior hinges on | `php artisan tinker --execute='var_export(config("<key>"));'` |
| The real exception + stack frames | `tail -n 200 storage/logs/laravel.log` (grep the exception class first) |

**Worked example.** Hypothesis: *"`Invoice::total()` omits tax because `tax_rate` is null for invoices created before the 2026-03 migration."*

1. Tinker `SELECT id, tax_rate` on pre-migration rows → null on old rows.
2. Tinker `[Invoice::find(<old>)->total(), Invoice::find(<new>)->total()]` → old omits tax.
3. Tinker one-shot recomputing the old total with a non-null rate substituted → tax appears. **Counterfactual closed → certified.** Paste all three transcripts into the report.

If `mcp__*boost*__*` tools are already connected they are acceptable equivalents — but never require Boost or ask the user to install it.

## Other stacks

Same principle with the stack's native REPL/test runner (`node --eval`, `python -c`/`pdb`, device + Metro logs for RN, failing test with the assertion diff verbatim). If neither a reproduction nor an isolated counterfactual is achievable, route to `UNCERTIFIED` rather than guessing.

## When certification fails

A failed certification is a *success* — a fake root cause caught before it cost a fix cycle. Hand the next round: the refuted hypothesis, the exact evidence that killed it, and which leads are now excluded. After the round cap without a certified cause, write the report `UNCERTIFIED` with the full history — never let an uncertified hypothesis reach `/quick` or `/plan` dressed as a diagnosis.
