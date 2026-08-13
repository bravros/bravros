# Test Runners Reference

Bundled with the `context` skill — snippets from here get passed into `claudemd-author` workers via the `docs` field. Standard runner CLIs (jest, vitest, pytest, go test, rspec, cargo test) need no documentation — the model knows them. What matters below is detection priority and the operator's own conventions.

## Detection

Detect from dev-dependency markers + config files. Polyglot priority order: Pest → PHPUnit → Jest → Vitest → pytest → RSpec → go test → cargo test.

## Laravel (Pest) — operator conventions

| Command Type | Command |
|--------------|---------|
| Targeted test (agent runs) | `vendor/bin/pest --filter="TestName"` or a specific file path |
| Full suite (USER runs, separate terminal) | `ptp` (alias: `./vendor/bin/pest --parallel --processes=10`) |
| Coverage targeted | `tc --filter="TestName"` (alias: `herd coverage ./vendor/bin/pest --coverage --filter=...`) |
| Coverage full (user runs) | `tcq` |
| Formatter | `vendor/bin/pint --dirty` |

- Operator shell aliases: `pt`, `ptp`, `tc`, `tcq`
- Never run the full suite unparallelized
- In a Herd environment prefix with `herd php`: `herd php vendor/bin/pest --filter="X"`
- Pest preferred over PHPUnit in Laravel projects

## Universal Rules

- **Agent runs targeted tests only** (single test / single file); **the user runs the full suite in a separate terminal**, always parallel.
- **Never mock what you can test** — real implementations, real databases, real factories.
- **100% coverage is the goal** on new projects; improve incrementally on older ones. Cover happy path, validation, authorization, edge cases, error handling.
- Formatter detection: use whatever the project's dev-dependencies declare (pint / prettier / eslint / black / ruff / rubocop / gofmt / cargo fmt); if none, skip formatting.
