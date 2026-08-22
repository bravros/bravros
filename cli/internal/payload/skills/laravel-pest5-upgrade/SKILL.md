---
name: laravel-pest5-upgrade
description: Upgrade a PHP/Laravel repo from Pest 4 to Pest 5 and turn on Test Impact Analysis (TIA) so local full runs only re-execute the tests a change can reach. Use this whenever someone mentions upgrading Pest, moving to Pest 5, `pestphp/pest ^5`, Test Impact Analysis, `--tia`, "why is my test suite so slow", speeding up a slow PHP test suite, `Tia mode requires Pest tests`, `WorkerCrashedException` after enabling TIA, converting PHPUnit class-style tests to Pest, or setting up a TIA baseline — even if they don't say "Pest 5" outright. Also use it when a Pest/PHPUnit dependency bump has broken tests and they need help attributing the fallout.
---

# Pest 4 → 5 + Test Impact Analysis

Turn a full local run into one that executes only the tests the current diff can reach. Measured
on a real 18,594-test Laravel suite: **655 s cold → 5.83 s** for a one-file change.

The version bump is nearly free. Almost all the work is step 2, and almost all the pain is traps
that surface as unrelated failures. Work in order — each checkpoint is what makes the next step
diagnosable.

**Scope: local adoption.** CI baseline sharing (`->baselined()` plus a workflow publishing the
graph) is out of scope — it was attempted, three bugs were found in the upstream recipe, and it
was parked. If asked for it, read `references/ci-baseline.md` first.

## 1. Preconditions

Needs PHP 8.4+ (`pest@5` requires `^8.4`, pulls `phpunit ^13.3`), a **coverage driver** (pcov or
Xdebug — TIA records its graph through it and cannot run without one; prefer pcov, several times
faster), and a **green full suite** to attribute fallout against.

Then count the real work, because it sets the schedule — two class-style tests is an afternoon,
fifty is a different project:

```bash
grep -rln --include='*Test.php' -E '^\s*(final\s+)?(abstract\s+)?class\s+\w+Test\b' tests/
```

## 2. Upgrade

```bash
composer update pestphp/pest --with-all-dependencies   # narrow, not a bare `composer update`
```

**Expect unrelated fallout.** Even the narrow form moves transitive dependencies. When a test
fails after the bump, diff **`composer.lock`**, not `composer.json`, and check whether the failing
subsystem's package moved — assuming "Pest 5 broke it" sends you hunting in the wrong codebase. On
the reference migration a docs package moved one patch version and its new route silently shadowed
one the app already served, producing a failure ~200 lines from its cause.

**Checkpoint:** `vendor/bin/pest --version` reports 5.x.

## 3. Convert every PHPUnit class-style test

TIA's hard prerequisite. It aborts the **entire run** on the first class-style test, and the
surfaced `WorkerCrashedException` names one arbitrary file while the real message hides inside:

```
ERROR  Tia mode requires Pest tests.
Encountered PHPUnit class Tests\Feature\...  (EnsureTiaIsRunningPestTestsOnly.php)
```

Anyone who hasn't seen this starts debugging a file that is fine.

**Checkpoint:** the step-1 grep returns nothing, and each converted file passes with the same
assertion count as before. Patterns, the helper-name collision trap, and dataset merges:
`references/conversion.md`.

## 4. Configure TIA

```php
pest()->tia()
    ->locally()                  // every local FULL run; no --tia flag needed
    ->filtered()                 // narrow PHPUnit to affected files -> real wall-time saving
    ->defaultBranch('homolog');  // omit when the repo's base really is main
```

- **`defaultBranch` is the one people get wrong.** TIA resolves the base from `origin/HEAD`,
  normally `main`. If feature branches are cut from a staging branch instead, every unpromoted
  commit counts as "changed" and TIA over-selects on every run. Check what branches are actually
  based on.
- **`locally()` is auto-skipped on CI.** Don't use `always()`.
- **No `->baselined()`** without the CI job — it's dead config that sends the next reader hunting
  for a workflow that never existed.
- **Keep default storage.** Don't set `directory()`; the reasoning, plus why worktrees silently
  behave differently, is in `references/tia-internals.md`.

**Checkpoint:** a targeted run prints *"TIA does not apply to partial runs"*; a second consecutive
full run prints `No affected tests found`.

Worth knowing: **a `--tia` run after a rebase is a full run** — the rebase invalidates the graph,
so it re-records everything and counts as a genuine gate.

## 5. Tell everyone the gate changed

The step most likely to be skipped, with the worst failure mode. **A full local run is now
impact-filtered**, so `--no-tia` is the real pre-PR gate — and mandatory after any dependency bump.

TIA selects off a *coverage* graph, so it cannot see reflection, string-resolved container
bindings, config/settings reads, or Blade. **A filtered green is evidence about the diff, not
about the suite.** Write this into the project's testing docs; left undocumented, someone ships on
a green run that never executed the relevant test.

## 6. Expect latent flakes

A filtered suite runs the same test many times per session, so factory randomness that hid behind
"one roll per run" starts surfacing. Treat a new intermittent failure as expected fallout, not as
evidence the upgrade broke something. Fix it in the **fixture**, not a shared factory used across
dozens of files.

Measure the rate rather than eyeballing one green run — `->repeat(100)` is the built-in tool.
Methodology, and why datasets are *not* a flake fix: `references/flakes.md`.

## 7. Adopt what Pest 5 adds

Refresh bundled agent testing guidance (packaged skills keep describing Pest 4 until
re-installed), consider the new validation expectations, and retire any "never use `describe()`"
rule. Details and the traps in each: `references/pest5-features.md`.

---

| Reference | Read when |
|---|---|
| `references/conversion.md` | Doing step 3 |
| `references/tia-internals.md` | Choosing storage, working in worktrees, or someone proposes committing the graph |
| `references/flakes.md` | A flake surfaces |
| `references/pest5-features.md` | Doing step 7 |
| `references/ci-baseline.md` | Someone asks for CI baseline sharing or `->baselined()` |

Every number here was measured on one real migration. When you hit something uncovered, prefer
reading the installed `vendor/pestphp/pest` source over the published docs — on that migration the
docs' own CI recipe was self-contradictory, and only the source explained why.
