---
name: laravel-pest-rector
description: Adopt Pest's Rector plugin on a Pest 5 project and refactor tests into Pest matchers — scoped to tests/, dry-run gated. Use on /laravel-pest-rector, or when adding pest-plugin-rector.
---

# Pest Rector — refactor tests into Pest matchers

Turns raw assertions into Pest's own matchers across an existing Pest 5 suite:

```diff
-expect(count($array))->toBe(5);
-expect(array_key_exists('id', $array))->toBeTrue();
+expect($array)->toHaveCount(5)
+    ->toHaveKey('id');
```

60 rules, one set. It refactors tests that are **already Pest** — it does not convert PHPUnit
class-style tests (that is `/laravel-pest5-upgrade` step 3, and a hard prerequisite here).

Despite the name, nothing in this flow is Laravel-specific — it works on any Pest 5 PHP project.
The prefix keeps it beside its sibling skill.

## 1. Preconditions — both are hard gates

**Pest 5 installed.** `vendor/bin/pest --version` reports 5.x. Not there yet ⇒ `/laravel-pest5-upgrade` first.

**A green suite, unfiltered.** This rewrites hundreds of test files at once; without a known-good
baseline you cannot attribute a single failure afterwards.

```bash
./vendor/bin/pest --parallel --no-tia
```

`--no-tia` matters even here. If the repo followed `/laravel-pest5-upgrade`, `pest()->tia()->locally()`
makes a bare `--parallel` run impact-filtered — a green that proves nothing about the files Rector
is about to touch. **Every suite run in this skill is `--no-tia`.**

Working tree clean before step 4 — Rector rewrites in place with no backup, and a dirty tree makes
its diff unreviewable.

## 2. Install

```bash
composer require pestphp/pest-plugin-rector --dev
composer require rector/rector --dev
```

The plugin already depends on `rector/rector ^2.6.1`, so `vendor/bin/rector` arrives either way.
The second line is what the docs prescribe and it pins Rector at top level — worth having when the
plugin's own constraint later widens.

**Require v5.0.2 or newer.** v5.0.0–v5.0.1 shipped a `ChainExpectCallsRector` bug that collapsed
two `expect()` calls on an AST-equal *side-effectful* subject into a single chain, silently dropping
the second evaluation and turning passing tests red. Fixed in v5.0.2 by an `isSideEffectFree()`
guard. `composer require` takes the latest by default — confirm with
`composer show pestphp/pest-plugin-rector`.

Not this plugin's job: converting PHPUnit class-style tests to Pest. That is
`pestphp/pest-plugin-drift`, a separate package — and `/laravel-pest5-upgrade` step 3.

## 3. Write `rector.php` BEFORE invoking rector at all

⛔ **This is the step that decides whether the skill did anything at all.**

With no `rector.php` present, `vendor/bin/rector` asks *"No rector.php config found. Should we
generate it for you?"* — and **you do not get to decline it.** Symfony's question helper returns
the default answer when stdin is not a TTY, and an agent shell is not a TTY, so the prompt
auto-answers **yes**, writes the file, and exits SUCCESS *having processed nothing*. Every later
run then silently uses that generated config.

So the config has to exist before rector is invoked even once. There is no `rector init` to reach
for — the command does not exist in Rector 2.x. Write the file.

The generated one carries the project's whole source tree and **no Pest set**, so a `process`
against it applies generic Rector rules to your application while performing zero Pest refactoring.

Write this instead:

```php
<?php

declare(strict_types=1);

use Pest\Rector\Set\PestSetList;
use Rector\Config\RectorConfig;

return RectorConfig::configure()
    ->withPaths([__DIR__ . '/tests'])
    ->withSets([
        PestSetList::CODING_STYLE,
    ]);
```

Two things are load-bearing: `withPaths` names **only** `tests/`, and `withSets` registers
`PestSetList::CODING_STYLE`. Drop either and this becomes a generic refactor of unrelated code.

**A `rector.php` already exists?** Do not overwrite it — merge. Add `PestSetList::CODING_STYLE` to
its `withSets(...)`, and if its `withPaths(...)` reaches beyond `tests/`, stop and ask: this skill's
gates assume a test-only blast radius, and the existing config is someone's deliberate choice.

Measured cost of getting this wrong: `references/generated-config-trap.md`.

## 4. Preview, read the diff, then apply

```bash
vendor/bin/rector process --dry-run
```

**Read it before applying** — this is the review, and there is no other. Confirm three things:
every path is under `tests/`; the changes are Pest matcher conversions and expectation chaining;
the file count is in the range you expect for the suite's size.

**`--dry-run` exits non-zero when it finds changes** (`ExitCode::CHANGED_CODE`). Here that is the
success case, not a failure — never gate on `$?` alone at this step, and never let a wrapper read
it as an error and abort.

The preview is faithful. Rector re-runs the whole rule pipeline over each file until an iteration
changes nothing, and `--dry-run` uses that same loop, returning just before the write — so chained
effects, like a matcher rewrite that then enables a chain merge, are already in the diff. Only a
cross-file cascade could need a second pass, which is what step 5's final check catches.

**`->only()` markers get stripped.** `RemoveOnlyRector` is in the set, so any focused test left in
the working tree loses its `->only()`. Look for that in the dry-run diff before applying.

```bash
vendor/bin/rector process
```

In place, no backup — the write path is a plain overwrite. The git diff is the only undo.

**Format afterwards.** Rector writes from the AST and its output is poorly formatted:

```bash
vendor/bin/pint --dirty
```

**Chained expectations across different variables.** `ChainExpectCallsRector` merges expectations
on *different* values into one `->and()` chain by default. That is safe — `->and($b)` still
evaluates `$b` exactly as a second `expect($b)` would — so it is a style choice, not a risk. Turn
it off only if you dislike the shape:

```php
->withConfiguredRule(ChainExpectCallsRector::class, ['merge_different_variables' => false])
```

## 5. Prove no regression

```bash
./vendor/bin/pest --parallel --no-tia
```

Same command as step 1, same expected result. **`--no-tia` is not optional here** — Rector just
rewrote a large share of the suite, and a filtered run would grade its own homework.

Failures are almost always a rewritten assertion that changed meaning. Fix the test, do not revert
the whole pass — and if a rule is systematically wrong, exclude that rule rather than abandoning
the set.

Then confirm Rector has nothing left, which is the cross-file cascade check:

```bash
vendor/bin/rector process --dry-run
```

Expect zero changes. If it finds more, apply, format, and re-run the suite before committing.

## 6. Commit

Test-only refactor, so:

```bash
bravros commit "🧪 test: refactor tests to Pest matchers via Rector" \
  rector.php composer.json composer.lock tests/
```

Name the paths explicitly. If `git status` shows anything outside `tests/`, `rector.php`, and the
composer files, **stop** — step 3 was wrong and the blast radius escaped.

`♻️ refactor:` is the alternative if the diff is dominated by restructuring rather than assertions.
Not `📋 plan:` — that type is for `.planning/` bookkeeping and misfiles the change.

## Verify

```bash
git diff --stat HEAD~1 -- . ':(exclude)tests' ':(exclude)rector.php' ':(exclude)composer.*'
```

Empty output = the refactor stayed in its lane. Anything listed means application code shipped
inside a commit labelled as a test change.

| Reference | Read when |
|---|---|
| `references/generated-config-trap.md` | Before step 3, or when a run touched app code |

Sources: [Rector plugin docs](https://pestphp.com/docs/rector) ·
[Pest 5 announcement](https://pestphp.com/docs/pest5-now-available#automated-refactoring-with-rector)
(observed 2026-08-22).
