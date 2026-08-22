# Adopting what Pest 5 adds

Read this while doing step 7. None of it is required for TIA to work — it's the rest of the
upgrade's value, plus two traps that bite during it.

## Refresh bundled agent testing guidance

Packaged skills that describe Pest keep describing **Pest 4** until re-installed. On Laravel Boost
projects, `php artisan boost:install --skills` rewrote the `pest-testing` skill with +59 lines
covering TIA, sharding and the new expectations. Skipping this leaves every agent in the repo
reading a stale reference, which is worse than having no skill at all — it will confidently
describe behaviour that no longer exists.

Two traps:

- **`--skills` alone does not touch `CLAUDE.md`.** A bare `boost:install` regenerates the full
  default guidelines block, which is destructive if the project hand-trimmed it. On the reference
  migration a trimmed block survived `--skills` byte-for-byte.
- **Diff `boost.json` afterwards.** The interactive run pruned four previously-selected skills and
  added three packages nobody asked for. The pruned entries turned out to be dangling symlinks
  into a gitignored directory — broken in every worktree and in CI — so the prune was arguably a
  fix, but it is not something you want to discover later by accident.

## New validation expectations

Shipping in `Pest\Mixins\Expectation`:

```php
expect('nuno@pestphp.com')->toBeEmail();
expect('01ARZ3NDEKTSV4RRFFQ69G5FAV')->toBeUlid();
expect('192.168.1.1')->toBeIpAddress();
expect('00:1a:2b:3c:4d:5e')->toBeMacAddress();
expect('example.com')->toBeHostname();
expect('example.co.uk')->toBeDomain();
expect('Zm9vYmFy')->toBeBase64();
expect('deadbeef')->toBeHexadecimal();
```

Worth grepping for hand-rolled equivalents to replace:

```bash
grep -rlE "FILTER_VALIDATE_EMAIL|Str::isUuid|preg_match.*@.*\\\\." tests/
```

> **Don't copy that list into the project's own docs.** Whatever packaged skill documents the
> framework API re-syncs itself on every install; a hand-copy in a repo's testing docs starts
> drifting the day Pest 6 adds more. Repo docs are for repo-specific rules; the framework API
> belongs to the skill that tracks it.

## Retire any "never use `describe()`" rule

Plenty of Pest 4-era codebases banned `describe()` blocks. Pest 5 supports datasets and
`beforeEach()` attached to a `describe()` block, which makes grouping genuinely useful, so the ban
is worth lifting as part of the upgrade.

Grep beyond the obvious file — these rules get restated in several places:

```bash
grep -rn "describe()" --include="*.md" . | grep -v vendor
```

Update only the **live** rule files. Leave planning dossiers, historical write-ups and completed
plan records alone: they record what was true when that work shipped, and rewriting them
falsifies the record. On the reference migration that meant editing two files and deliberately
leaving twelve historical mentions untouched.

Lifting the ban does not mean rewriting existing tests. Flat `it()` stays perfectly good; this
just removes a rule that no longer has a reason behind it.

## Version strings that quietly go stale

Any doc asserting the stack version needs updating, or it will mislead the next reader — and these
are exactly the lines nobody thinks to grep:

- `pest v4` → `v5`, `phpunit v12` → `v13` in generated framework-context blocks
- `**Framework:** Pest PHP v4` style lines in testing docs
- Test-status files claiming counts from a much older run — check the date stamp before trusting
  one as a baseline; on the reference migration it was three months stale and claimed half the
  current test count
