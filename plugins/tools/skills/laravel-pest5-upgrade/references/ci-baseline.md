# CI baseline sharing — read before attempting it

The idea: CI records the TIA graph once and publishes it as an artifact, so `->baselined()` lets
every developer skip the cold recording pass. Genuinely useful when it works.

It was attempted on a real repo and **parked**. Three independent bugs in the upstream recipe, each
found only by running it. If someone asks for this, start here so you don't rediscover them.

## The upstream starter workflow does not work as published

| Published recipe | What actually happens | Fix |
|---|---|---|
| `cp .env.example .env` + `php artisan key:generate` (or nothing at all) | `.env.example` typically points cache/session at Redis. `composer install`'s `post-autoload-dump` runs `artisan package:discover`, which **boots the framework** — so the job dies with `Connection refused [tcp://127.0.0.1:6379]` before a single test runs | Set `APP_ENV: testing` at job level and let Laravel load the repo's tracked `.env.testing`. No `cp`, no `key:generate` — that file already carries a throwaway `APP_KEY` and array/sqlite drivers |
| `--tia --coverage --fresh` | **Self-contradictory.** `--fresh` wipes the graph; `--coverage` requires one to exist. In `Tia.php`: `if (! $graph instanceof Graph && $this->piggybackCoverage) { skip; return; }`. The job runs the full suite, **exits 0**, and uploads an empty artifact | Drop `--coverage`. Pest says so itself in the run output: *"Record the baseline with a plain `--tia` run first; coverage runs then reuse it"* |
| `push: { branches: [main] }` | Fine as published. But adapting it to a staging branch fires on **every merge**, and the check attaches to the head commit — so it **gates the staging→main PR** | Keep the `main` push trigger plus the nightly cron. `ref: <staging>` on checkout already decouples *what gets recorded* from *what triggers the run* |

The second one is the dangerous shape and worth internalising: **a green check that produced
nothing.** No error, exit 0, empty artifact, and `baselined()` downstream silently finding no
graph. Only a line in the log reveals it.

> If you adopt this: verify the uploaded artifact actually contains `graph.json`. A green check is
> not proof.

## Three names are load-bearing

Pest looks these up by string; a typo produces silence, not an error:

- the workflow **filename** `tia-baseline.yml` (`DEFAULT_WORKFLOW_FILE`)
- the artifact name `pest-tia-baseline`
- `graph.json` at the artifact root — which `path: <dir>` produces, since `--baseline` prints a
  directory

`include-hidden-files: true` is required because the baseline lives under a dot-prefixed directory.

## What was still unsolved at the point of parking

CI-only test failures that a local `--parallel --processes=10` run did not reproduce. The measured
local-vs-CI comparison, in the order worth checking:

| | Local (passing) | CI (failing) |
|---|---|---|
| Worker count | **10** (pinned) | **~4** (runner CPUs) — reshards test files; can expose parallel-safety bugs invisible at another count |
| PHP extensions | `redis` `imagick` `soap` `gmp` `sockets` `pcntl` present | none of them — `soap` was needed by one carrier test |
| Services | Redis + MySQL running | none |
| Env files | `.env` **and** `.env.testing` | `.env.testing` only |
| PHP timezone | UTC | UTC — **ruled out by measurement** |

Also ruled out: the ~20-key gap between `.env` and `.env.testing`, because `phpunit.xml`'s own
`<env>` overrides normalise the ones that matter (cache, session, queue, DB, mail) in both.

The most probable cause was the worker-count difference. Pin `--processes` in CI to match local
before assuming anything more exotic.

## Two things worth doing regardless

- **`--tia` never belongs on the PR pipeline.** Pest's docs: *"The Tia Engine is built for local
  development, and you should not add `--tia` to the command that runs your test suite on CI."* The
  baseline job is the single sanctioned exception; PR CI keeps running the full suite.
- **A parked workflow should be parked properly.** Reduce its triggers to `workflow_dispatch:` only
  and leave a comment explaining what was fixed, what remains, and how to resume. A disabled-but-
  documented workflow is recoverable; a deleted one loses the three bug fixes with it.
