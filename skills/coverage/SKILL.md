---
name: coverage
description: >
  Analyze test coverage gaps across any framework (Pest, Jest, pytest, Go, etc.) and suggest tests to reach 100% coverage.
  Use this skill whenever the user says "/coverage", "check coverage", "test coverage",
  "coverage analysis", or any request to analyze or improve test coverage.
  Also triggers on "coverage gaps", "untested code", "missing tests", or "coverage report".
  Framework-agnostic — auto-detects Pest, Jest, Vitest, pytest, Go test, RSpec, Cargo test.
  Agent runs targeted coverage only — full suite must be run by the user in a separate terminal.
---

# Coverage: Test Coverage Analysis

Analyze test coverage gaps and create tests to close them. Auto-detects test framework and uses appropriate coverage commands.

## Model Requirement

**Sonnet 4.6** — this skill performs mechanical/scripted operations that don't require deep reasoning.

## Step 0/1: Detect Test Runner

```bash
echo "🧪 coverage [0/1] detecting test runner"
TEST_RUNNER=$(~/.claude/bin/bravros meta --field stack.test_runner 2>/dev/null)
# Enriched: exact version for Context7 docs
TEST_RUNNER_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.$(~/.claude/bin/bravros meta --field stack.test_runner) 2>/dev/null)
```

Store the detected runner and use it for all coverage commands below. When resolving docs via `mcp__context7__resolve-library-id`, include the version in the query (e.g. "pest $TEST_RUNNER_VERSION") for version-accurate documentation.

## Coverage Goals

- **New projects:** 100% coverage — no exceptions, no excuses
- **Older projects:** Improve incrementally — every PR should increase or maintain coverage
- **Every new file must have tests** — no untested code enters the codebase

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Running Coverage

### Targeted Coverage (Agent Runs)

**Laravel (Pest):**
```bash
vendor/bin/pest --coverage --filter="$ARGUMENTS"
tc --filter="$ARGUMENTS"
```
> **macOS with Herd:** Use `herd coverage vendor/bin/pest --coverage --filter="$ARGUMENTS"` for Herd-managed PHP.

**Jest / Vitest:**
```bash
npx jest --coverage --testPathPattern="$ARGUMENTS"
```

**pytest:**
```bash
pytest --cov -k "test_name_pattern"
pytest --cov tests/test_module.py
```

**Go test:**
```bash
go test -cover -run TestName ./...
```

**RSpec:**
```bash
bundle exec rspec spec/models/user_spec.rb --example "test description"
```

**Cargo test:**
```bash
cargo tarpaulin --test test_name
```

### Full Coverage (User Runs in Separate Terminal)

Tell the user:
- **Laravel:** "Run `tcq` in your terminal and share the results" (alias for full parallel coverage)
- **JS:** "Run `npx jest --coverage` in your terminal"
- **Python:** "Run `pytest --cov` in your terminal"
- **Go:** "Run `go test -cover ./...` in your terminal"
- **Ruby:** "Run `bundle exec rspec` with SimpleCov gem in your terminal"
- **Rust:** "Run `cargo tarpaulin` in your terminal"

**NEVER run the full coverage suite from an agent.**

## After Receiving Results

1. **Identify uncovered files** — sort by lowest coverage first
2. **Identify uncovered methods** — which public methods have no test hitting them?
3. **Identify missing branches** — if/else, switch, try/catch, early returns
4. **Prioritize by risk:**
   - Business logic (payments, orders, auth) → cover first
   - Services and repositories → cover second
   - Controllers and Livewire components → cover third
   - Helpers and utilities → cover last

## Creating Tests to Close Gaps

Follow the `/test` skill rules:
- **Follow the project's test framework conventions** — use `it()`, `test()`, `describe()`, or `def test_()` as appropriate
- **NEVER mock what you can test** — real factories, real database, real implementations
- Explicit factory/fixture values for business-logic fields
- Test happy path, validation, authorization, edge cases, error handling

## What "100% Coverage" Actually Means

It's not just line coverage. For each class, verify:
- Every public method is called in at least one test
- Every conditional branch (if/else, ternary, null coalesce) has both paths tested
- Every exception/error path is tested
- Every validation rule is tested (both pass and fail)
- Every authorization check is tested (both allowed and denied)

## Rules

- NEVER run full coverage suite — ask user to run `tcq` in separate terminal
- NEVER mock what you can test
- NEVER suggest skipping tests or lowering coverage targets
- ALWAYS create tests for uncovered code (don't just report gaps)
- ALWAYS aim for 100% on new projects
- ALWAYS prioritize business-critical code first

## Interaction Rules

- All user interactions MUST use `AskUserQuestion` tool, never plain text questions

Use $ARGUMENTS as filter, file path, or class name to analyze.
