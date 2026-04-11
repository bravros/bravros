# Test Runners Reference

Reference file bundled with the `context` skill. Used for detecting and running tests across multiple stacks.

## Detection

### 1. Check for Package Manager Files (in order)

Start with language detection from `stack-detection.md`, then look for test runner markers.

### 2. Test Runner Priority (when multiple exist)

For polyglot projects, apply this priority order:
1. Pest (if `pest` in `composer.json`)
2. PHPUnit (if `phpunit/phpunit` in `composer.json`)
3. Jest (if `jest` in `package.json`)
4. Vitest (if `vitest` in `package.json`)
5. pytest (if `pytest` in `pyproject.toml` or `requirements.txt`)
6. RSpec (if `rspec` in `Gemfile`)
7. Go test (if `*_test.go` files exist)
8. Cargo test (if `#[cfg(test)]` in Rust files)

### 3. Test Config Files (for confirmation)

| Runner | Config File(s) |
|--------|-----------------|
| Pest | `phpunit.xml`, `pest.xml` |
| PHPUnit | `phpunit.xml` |
| Jest | `jest.config.js`, `jest.config.ts`, `jest.config.json` |
| Vitest | `vitest.config.js`, `vitest.config.ts` |
| pytest | `pytest.ini`, `pyproject.toml` (with `[tool.pytest]`), `setup.cfg` |
| RSpec | `spec_helper.rb`, `.rspec` |
| Go test | `*_test.go` files (go test is built-in) |
| Cargo test | `Cargo.toml` (cargo test is built-in) |

## Test Commands Per Stack

### Laravel (Pest) - Preferred
```
Language: PHP
Framework: Laravel
Package Manager: Composer
Installer: composer.json (require-dev: "pest/pest")
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `vendor/bin/pest --filter="TestName"` |
| **Specific test file** (agent runs) | `vendor/bin/pest tests/Feature/Path/ToTest.php` |
| **Full suite** (user runs in separate terminal) | `ptp` (alias for `./vendor/bin/pest --parallel --processes=10`) |
| **Coverage (targeted)** | `tc --filter="TestName"` (alias for `herd coverage ./vendor/bin/pest --coverage --filter="TestName"`) |
| **Coverage (full)** | `tcq` (alias for `herd coverage ./vendor/bin/pest --coverage --parallel --processes=10`) |
| **Code formatter** | `vendor/bin/pint --dirty` (check dirty files) or `vendor/bin/pint` (all files) |

**Notes:**
- User has shell aliases: `pt`, `ptp`, `tc`, `tcq`
- Never run full suite unparallelized (blocked by audit hook)
- Pest is preferred over PHPUnit in Laravel projects
- Use `herd php` prefix if in Herd environment: `herd php vendor/bin/pest --filter="X"`

### Laravel (PHPUnit)
```
Language: PHP
Framework: Laravel
Package Manager: Composer
Installer: composer.json (require-dev: "phpunit/phpunit")
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `vendor/bin/phpunit --filter="TestName"` |
| **Specific test file** (agent runs) | `vendor/bin/phpunit tests/Feature/Path/ToTest.php` |
| **Full suite** (user runs) | `vendor/bin/phpunit --parallel` |
| **Coverage (targeted)** | `vendor/bin/phpunit --coverage-text --filter="TestName"` |
| **Coverage (full)** | `vendor/bin/phpunit --coverage-text` (user runs) |
| **Code formatter** | `vendor/bin/pint --dirty` |

**Notes:**
- Less common in modern Laravel; Pest is preferred
- Can also use: `vendor/bin/phpunit --testdox` for readable output

### Next.js / React (Jest)
```
Language: JavaScript / TypeScript
Framework: Next.js or React
Package Manager: npm / yarn / pnpm
Installer: package.json ("jest" in devDependencies)
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `npx jest --testPathPattern="X"` |
| **Specific test file** (agent runs) | `npx jest tests/unit/component.test.js` |
| **Full suite** (user runs) | `npx jest` |
| **Coverage (targeted)** | `npx jest --coverage --testPathPattern="X"` |
| **Coverage (full)** | `npx jest --coverage` (user runs) |
| **Code formatter** | `npx prettier --write .` or `npx eslint --fix .` |

**Notes:**
- Check `jest.config.js` for test path patterns (often `__tests__` or `*.test.js`)
- Use `--testNamePattern="pattern"` for test name filtering (alternative to `--testPathPattern`)

### Next.js / React (Vitest)
```
Language: JavaScript / TypeScript
Framework: Next.js or React
Package Manager: npm / yarn / pnpm
Installer: package.json ("vitest" in devDependencies)
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `npx vitest run tests/unit/component.test.js` |
| **Specific test file** (agent runs) | `npx vitest run tests/unit/component.test.js` |
| **Full suite** (user runs) | `npx vitest run` |
| **Watch mode** | `npx vitest` (interactive) |
| **Coverage (targeted)** | `npx vitest run --coverage tests/unit/component.test.js` |
| **Coverage (full)** | `npx vitest run --coverage` (user runs) |
| **Code formatter** | `npx prettier --write .` or `npx eslint --fix .` |

**Notes:**
- Vitest is faster and Vite-native (preferred for Vite projects)
- Default mode is watch; use `vitest run` for CI/one-time runs

### React Native / Expo (Jest)
```
Language: JavaScript / TypeScript
Framework: React Native or Expo
Package Manager: npm / yarn / pnpm
Installer: package.json ("jest" in devDependencies)
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `npx jest --testPathPattern="X"` |
| **Specific test file** (agent runs) | `npx jest app/screens/__tests__/Home.test.js` |
| **Full suite** (user runs) | `npx jest` |
| **Coverage (targeted)** | `npx jest --coverage --testPathPattern="X"` |
| **Coverage (full)** | `npx jest --coverage` (user runs) |
| **E2E tests** | `npx detox test` (if Detox installed) |
| **Code formatter** | `npx prettier --write .` |

**Notes:**
- 100% coverage is the goal (same as web)
- Jest preset: `react-native` (check `jest.config.js`)
- E2E: Detox is common; runs on simulator/device

### Python (pytest)
```
Language: Python
Package Manager: pip / Poetry / uv
Installer: pyproject.toml or requirements.txt ("pytest")
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `pytest -k "test_name_pattern"` |
| **Specific test file** (agent runs) | `pytest tests/test_module.py` |
| **Full suite** (user runs) | `pytest` or `pytest tests/` |
| **Verbose output** | `pytest -v` |
| **Coverage (targeted)** | `pytest --cov -k "test_name_pattern"` |
| **Coverage (full)** | `pytest --cov` (user runs) |
| **Code formatter** | `black .` or `ruff format .` |
| **Linter** | `ruff check .` |

**Notes:**
- Check `pytest.ini` or `pyproject.toml` for test discovery patterns
- Common patterns: `tests/` or `test_*.py`
- `pytest-cov` plugin for coverage: `pip install pytest-cov`

### Go (go test)
```
Language: Go
Package Manager: go.mod
Built-in Test Runner: go test
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `go test ./... -run TestName` |
| **Specific package** (agent runs) | `go test ./pkg/users` |
| **Full suite** (user runs) | `go test ./...` |
| **Verbose output** | `go test -v ./...` |
| **Coverage (targeted)** | `go test -cover -run TestName ./...` |
| **Coverage (full)** | `go test -cover ./...` (or `go test -coverprofile=coverage.out ./...`) |
| **HTML coverage report** | `go tool cover -html=coverage.out` |
| **Code formatter** | `gofmt -w .` or `goimports -w .` |
| **Linter** | `golangci-lint run` |

**Notes:**
- Go test is built-in; no installation needed
- Test files: `*_test.go` (same package as source)
- Benchmark tests: `go test -bench=. ./...`

### Ruby on Rails (RSpec)
```
Language: Ruby
Framework: Rails
Package Manager: Bundler
Installer: Gemfile ("rspec-rails")
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `bundle exec rspec --example "TestName"` |
| **Specific test file** (agent runs) | `bundle exec rspec spec/models/user_spec.rb` |
| **Full suite** (user runs) | `bundle exec rspec` |
| **Verbose output** | `bundle exec rspec -fd` |
| **Coverage (targeted)** | `bundle exec rspec --example "TestName"` (with SimpleCov gem) |
| **Coverage (full)** | `bundle exec rspec` (user runs, with SimpleCov) |
| **Code formatter** | `bundle exec rubocop -a` |

**Notes:**
- RSpec is the standard Rails testing framework
- Test directory: `spec/`
- Models, Controllers, Features, Requests: `spec/models/`, `spec/controllers/`, `spec/features/`, `spec/requests/`
- SimpleCov for coverage (add to `spec_helper.rb`)

### Rust (cargo test)
```
Language: Rust
Package Manager: Cargo
Built-in Test Runner: cargo test
```

| Command Type | Command |
|--------------|---------|
| **Targeted test** (agent runs) | `cargo test test_name` |
| **Specific module** (agent runs) | `cargo test module::submodule::` |
| **Full suite** (user runs) | `cargo test` |
| **With output** | `cargo test -- --nocapture` |
| **Coverage (targeted)** | `cargo tarpaulin --test test_name` |
| **Coverage (full)** | `cargo tarpaulin` (user runs) |
| **Code formatter** | `cargo fmt` |
| **Linter** | `cargo clippy` |

**Notes:**
- Tests are inline: `#[cfg(test)] mod tests { #[test] fn test_name() {} }`
- Or separate: `tests/` directory for integration tests
- `cargo tarpaulin` for coverage (install: `cargo install cargo-tarpaulin`)

## Test File Patterns

| Stack | Directory | File Pattern | Example |
|-------|-----------|--------------|---------|
| **Laravel** | `tests/` | `*Test.php` | `tests/Feature/UserTest.php` |
| **Next.js/React** | `__tests__/` or same dir | `*.test.js`, `*.test.ts`, `*.spec.js` | `components/__tests__/Button.test.js` or `components/Button.test.js` |
| **React Native/Expo** | `__tests__/` or same dir | `*.test.js`, `*.test.ts` | `app/screens/__tests__/Home.test.js` |
| **Python** | `tests/` | `test_*.py`, `*_test.py` | `tests/test_models.py` |
| **Go** | same as source | `*_test.go` | `user_test.go` (in same dir as `user.go`) |
| **Ruby/Rails** | `spec/` | `*_spec.rb` | `spec/models/user_spec.rb` |
| **Rust** | `tests/` or inline | `*_test` in code, or `tests/*.rs` | Inline: `#[test]` or `tests/integration_test.rs` |

## Finding Tests for a Source File

**Strategy 1: Pattern mapping**
- Source: `app/Models/User.php` → Test: `tests/Unit/Models/UserTest.php`
- Source: `src/components/Button.tsx` → Test: `src/components/__tests__/Button.test.tsx`
- Source: `user.go` → Test: `user_test.go` (same directory)

**Strategy 2: Search by class/function name**
- `grep -r "class UserTest" tests/`
- `grep -r "describe('User'" tests/`
- `grep -r "test_user" tests/`

**Strategy 3: Test discovery**
- Run test runner with verbose flag: `vendor/bin/pest -v` (shows all tests)
- Check test file count: `find tests/ -name "*.php" -o -name "*.js" | wc -l`

## Common Rules (Universal)

### Agent vs. User Responsibilities
- **Agent runs**: Targeted tests (single test, single file) for fast feedback
- **User runs in separate terminal**: Full test suite (always parallel, never serial)
- **Reason**: Agent context is limited; user's terminal is already running `npm run dev` or similar

### Testing Philosophy
- **Never mock what you can test** — use real implementations, real databases, real factories
- **100% coverage is the goal** on new projects; improve incrementally on older projects
- Test all branches:
  - Happy path (success case)
  - Validation (invalid input)
  - Authorization (access control)
  - Edge cases (boundaries, null, empty)
  - Error handling (exceptions, timeouts)

### Test Output
- Use `--filter` or `--testNamePattern` for readable test names
- Avoid `--quiet` unless user requests; include assertions in output
- Show line numbers and file paths for failures

## Formatter Detection

### 1. Check `composer.json` (PHP)
```json
{
  "require-dev": {
    "laravel/pint": "^1.13"
  }
}
```
→ Formatter: `vendor/bin/pint`

### 2. Check `package.json` (JavaScript/TypeScript)
```json
{
  "devDependencies": {
    "prettier": "^3.0"
  }
}
```
→ Formatter: `npx prettier --write .`

Or ESLint:
```json
{
  "devDependencies": {
    "eslint": "^8.0"
  }
}
```
→ Formatter: `npx eslint --fix .`

### 3. Check `pyproject.toml` (Python)
```toml
[tool.black]
line-length = 88
```
→ Formatter: `black .`

Or Ruff:
```toml
[tool.ruff]
target-version = "py38"
```
→ Formatter: `ruff format .`

### 4. Check `Gemfile` (Ruby)
```ruby
gem 'rubocop', require: false
```
→ Formatter: `bundle exec rubocop -a`

### 5. Go (built-in)
→ Formatter: `gofmt -w .` or `goimports -w .`

### 6. Rust (built-in)
→ Formatter: `cargo fmt`

### 7. Fallback
If no formatter detected: Skip formatting, note in logs: `[context:formatter] No formatter detected`

## Example: Full Detection Flow

```bash
# Step 1: Detect language
[context:1] Checking composer.json... found
[context:1] Language: PHP

# Step 2: Detect framework
[context:1] Checking composer.lock... laravel/framework found
[context:1] Framework: Laravel 11.x

# Step 3: Detect test runner
[context:1] Checking composer.json (require-dev)... pest found
[context:1] Test runner: Pest PHP

# Step 4: Detect formatter
[context:1] Checking composer.json (require-dev)... laravel/pint found
[context:1] Formatter: Laravel Pint

# Summary
[context:1] Stack detected:
[context:1]   Language:    PHP 8.3
[context:1]   Framework:   Laravel 11.x
[context:1]   Test runner: Pest PHP
[context:1]   Formatter:   Laravel Pint
```
