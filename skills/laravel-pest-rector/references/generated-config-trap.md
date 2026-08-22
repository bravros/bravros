# The generated `rector.php` trap — measured, not theoretical

Every number here comes from one real adoption on a ~7,500-test Laravel suite (afterpay,
2026-08-22, commit `a6e15ea9`) where the generated config was accepted.

## You cannot decline it from an agent shell

`ProcessCommand` calls `ConfigInitializer::createConfig()`, which asks via Symfony's
`SymfonyStyle::ask(...)` with a default of **yes**. Symfony's `QuestionHelper` returns the default
without prompting whenever stdin is not interactive — and an agent's bash is not a TTY. So the
"offer" is accepted automatically, the file is written, and the command exits SUCCESS *having
refactored nothing*. The operator sees a clean exit and a new `rector.php`; the next run silently
adopts it.

There is also no `rector init` to reach for — Rector 2.x ships `process`, `list-rules`,
`custom-rule`, `setup-ci` and `worker`, and nothing else. Writing the file yourself is the only
deterministic path.

## What the generator produces

Accepting (or being auto-accepted into) generation gives:

```php
return RectorConfig::configure()
    ->withPaths([
        __DIR__ . '/app',
        __DIR__ . '/bootstrap',
        __DIR__ . '/config',
        __DIR__ . '/lang',
        __DIR__ . '/public',
        __DIR__ . '/resources',
        __DIR__ . '/routes',
        __DIR__ . '/tests',
    ])
    // uncomment to reach your current PHP version
    // ->withPhpSets()
    ->withTypeCoverageLevel(0)
    ->withDeadCodeLevel(0)
    ->withCodeQualityLevel(0);
```

It is a sensible default **for adopting Rector across an application**. It is the wrong config for
this skill, in two independent ways:

1. **`withPaths` covers the whole app** — `app/`, `bootstrap/`, `config/`, `resources/`, `routes/`
   as well as `tests/`. The blast radius is the entire codebase, including Blade views.
2. **No `PestSetList`.** The Pest plugin's 60 rules are never registered, so installing
   `pestphp/pest-plugin-rector` has no effect whatsoever on the run.

## What that produced

| | |
|---|---|
| Files changed | **562** (6,415 insertions / 6,116 deletions) |
| `tests/` | 276 |
| `app/` | 10 — controllers, jobs, models, services |
| `resources/` | 13 — **Blade views** |
| `routes/` | 4 |
| New `: void` closure return types | **5,986** |
| Pest matcher conversions (`toHaveCount`, `toHaveKey`) | **0** |

Zero. The entire point of the plugin — raw assertions into Pest matchers — did not happen, while
6,116 lines of unrelated churn did.

Representative test-file change, which is the *only* kind it made:

```diff
-test('admin dashboard page loads without errors', function () {
+test('admin dashboard page loads without errors', function (): void {
```

And in application code, from `app/Services/OrderRiskService.php`:

```diff
-            ->whereHas('client', function ($q) use ($document, $clientId) {
+            ->whereHas('client', function ($q) use ($document, $clientId): void {
```

Harmless individually. But 27 non-test files were rewritten inside what was intended as a test
refactor, and the commit that carried them was typed `📋 plan: tests updated` — a production-code
change filed under planning bookkeeping, invisible to anyone scanning the log for code changes.

## Why it is easy to miss

Nothing fails. `rector process` exits 0, the suite still passes (adding `: void` to a closure that
returns nothing is safe), and the diff looks like work got done. The only signal that the Pest
rules never ran is the **absence** of matcher conversions — which you have to go looking for.

## The check

After a run, before committing:

```bash
git diff --stat -- . ':(exclude)tests' ':(exclude)rector.php' ':(exclude)composer.*'   # must be empty
git diff -- tests/ | grep -cE '^\+.*(toHaveCount|toHaveKey|->and\()'                    # must be > 0
```

First command empty proves the scope held. Second above zero proves `PestSetList::CODING_STYLE`
actually ran. A run that satisfies one but not the other is misconfigured, whichever way round.
