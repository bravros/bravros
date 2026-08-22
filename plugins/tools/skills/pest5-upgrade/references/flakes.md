# Flakes surfaced by the upgrade

Read this when an intermittent failure appears after enabling TIA.

## Why they surface now

A filtered suite runs the same test many times in a working session, where a full suite ran it once.
Factory randomness that used to hide behind "one roll per run" starts landing on its unlucky value.
The upgrade didn't create these — it stopped concealing them. Treat a new flake as expected fallout.

## Measure the rate before and after

One green run proves nothing about a 1-in-20 flake, and "I ran it again and it passed" is how flakes
survive for months. Two ways to get a number:

```php
it('...', function () { /* ... */ })->repeat(100);   // temporary, while hunting
```

```bash
fails=0
for i in $(seq 1 30); do
  vendor/bin/pest <file> --filter="<name>" --compact 2>&1 | grep -q "1 passed" || fails=$((fails+1))
done
echo "$fails / 30"
```

Run it before the fix and after. On the reference migration that was 1/20 → 0/30 — a number worth
putting in the PR, because it's the difference between "fixed" and "didn't reproduce this time".

## Fix the fixture, not the shared factory

The reference case: a test deleted all `Embalagem` rows in `beforeEach` and created exactly one
deterministic box, then called `Pacote::factory()` — whose default `embalagem_id` is
`Embalagem::factory()`, silently creating a *second* box with a random weight. The selection logic
picks the smallest box that fits, so whenever the random one's volume landed in range it won, and a
weight assertion failed with a different number every time.

The fix was one line in the fixture:

```php
'embalagem_id' => Embalagem::query()->value('id'),
```

**Not** a change to `EmbalagemFactory`. That factory was used bare in 74 other test files; changing
its default distribution has a far larger blast radius than the one failing test. The general rule:
when a shared factory produces a value that breaks one test, pin it at the call site.

The trap worth remembering is the *transitively created* model — the test never named `Embalagem`
when calling `Pacote::factory()`, so nothing in the test file hinted at where the randomness came
from. When a numeric assertion drifts, check what the factories you call create in turn.

## Read the dependency's source before calling it a bug

Same case, a lesson that cost more than the fix. The factory read:

```php
'peso' => $faker->randomFloat(3, 1, 0.01),   // looks like inverted min/max
```

It was written up as a factory bug. It isn't — Faker explicitly swaps `$min`/`$max` when
`$min > $max` (`Provider/Base.php`), so the range really is 0.01–1. Sampling 200 values confirmed
`min=0.011 max=0.996`. The arguments are confusingly *ordered*, not broken, and "fixing" them would
have changed nothing while implying the library was at fault.

Before writing up a bug in a dependency: read the source, or sample the behaviour. Both are cheap.

## Datasets are not a flake fix

Conflating these wastes time, so be precise:

| Tool | What it actually does |
|---|---|
| `->with([...])` (datasets) | Replaces N copy-pasted tests with one and *names* each case, so a failure says which input broke. Removes randomness **only** where you swap faker for literal dataset values |
| `->repeat(N)` | **Detects** a flake — surfaces the 1-in-20 |
| A deterministic fixture | **Fixes** it |

A dataset parameterizes inputs the test *declares*. It would not have caught the case above, where
the randomness came from a model the test never named. Converting to datasets is still worth doing
for readability, and it does help wherever a test currently leans on faker for its own inputs — just
not as a flake strategy.

## Flakes that are environmental, not random

If a test passes locally and fails only in another environment, the cause is usually configuration,
not randomness. Compare, in this order: worker count (a different `--processes` value reshards test
files and can expose parallel-safety bugs that never appear at another count), loaded PHP
extensions, running services (Redis, MySQL), and which env file is loaded. Timezone is worth ruling
out early — it's cheap to check and a common suspect, but a project pinning `config('app.timezone')`
makes it a non-issue.
