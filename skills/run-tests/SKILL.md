---
name: run-tests
description: >
  Run targeted tests across any framework, handle failures, and report results.
  Use this skill whenever the user says "/run-tests", "run tests", "test this",
  "run the tests", or any request to execute existing tests.
  Also triggers on "check tests", "are tests passing", "test suite", or "verify tests".
  Framework-agnostic — auto-detects Pest, Jest, Vitest, pytest, Go test, RSpec, Cargo test.
  Agent runs targeted tests only — full suite is always run by the user in a separate terminal.
---

# Run Tests: Execute and Report

Run targeted tests and report results. Auto-detects test framework and uses appropriate commands.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

**Sonnet 4.6** — this skill performs mechanical/scripted operations that don't require deep reasoning.

## Step 0: Detect Test Runner

```bash
TEST_RUNNER=$(~/.claude/bin/bravros meta --field stack.test_runner 2>/dev/null)
# Enriched: exact version for Context7 docs
TEST_RUNNER_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.$(~/.claude/bin/bravros meta --field stack.test_runner) 2>/dev/null)
```

Store the detected runner and use it for all test commands below. When resolving docs via `mcp__context7__resolve-library-id`, include the version in the query (e.g. "pest $TEST_RUNNER_VERSION") for version-accurate documentation.

## Critical Rules

- **Agent runs targeted tests only** — specific file or framework-specific filter
- **Full suite: ask user to run the project's full test command in a separate terminal** — never run from an agent
- **NEVER mock what you can test** — if a fix involves adding a mock, reconsider
- **NEVER run unparallelized** (for frameworks that support it)

## Running Tests

### Laravel (Pest)
```bash
vendor/bin/pest --filter="TestName"
vendor/bin/pest tests/Feature/SomeTest.php
```

### Jest / Vitest
```bash
npx jest --testPathPattern="SomeTest"
npx jest src/__tests__/SomeTest.test.js
```

### pytest
```bash
pytest -k "test_name_pattern"
pytest tests/test_module.py
```

### Go test
```bash
go test ./... -run TestName
go test ./package
```

### RSpec
```bash
bundle exec rspec spec/models/user_spec.rb --example "test description"
```

### Cargo test
```bash
cargo test test_name
```

## Full Suite (User Runs)

Tell user: **"Run the project's full test command in a separate terminal"**
- Laravel: `ptp` (pre-configured alias for parallel run) — or `./vendor/bin/pest --parallel --processes=10`
- JS: `npm test` or `npx jest` or `npx vitest`
- Python: `pytest tests/`
- Go: `go test ./...`
- Ruby: `bundle exec rspec`
- Rust: `cargo test`

## On Failures

1. Read the error message carefully
2. Check common flaky patterns:
   - Random factory/fixture values → use explicit values for business-logic fields
   - Hardcoded test data too generic → make more specific
   - Random IDs causing skips → set explicit values
   - Missing seed data → add to setup/beforeEach/fixtures
3. **Fix the root cause** — never skip tests, never add mocks to make it pass
4. Re-run the targeted test to confirm fix
5. Ask user to run full suite to verify nothing else broke

## Reporting

After running, show a concise summary (do NOT dump raw test output):
- Pass/fail count
- Failures with file:line and error message
- Suggested fix for each failure
- If all pass, use `AskUserQuestion`: "All N tests pass. Run full suite?" with options matching the detected framework

## Interaction Rules

- All user interactions MUST use `AskUserQuestion` tool, never plain text questions

Use $ARGUMENTS as test filter, file path, or class name. If no arguments provided, detect recently changed files with `git diff --name-only HEAD` and run tests for those files. If no changes detected either, use `AskUserQuestion` to ask what to test.
