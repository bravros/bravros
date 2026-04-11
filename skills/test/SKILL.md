---
name: test
description: >
  Create tests for the project's detected test framework (Pest, Jest, Vitest, pytest, Go test, etc.).
  Use this skill whenever the user says "/test", "write tests", "create tests",
  "add tests for", or any request to create new test files.
  Also triggers on "test this", "write a test for", "feature test", "unit test",
  "jest test", "pytest", "create unit tests", or any request to generate test code.
  Framework-agnostic — auto-detects Pest, Jest, Vitest, pytest, Go test, RSpec, Cargo test.
---

# Test: Create Tests

Create tests for: $ARGUMENTS

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 0: Detect Test Framework

```bash
FRAMEWORK=$(~/.claude/bin/bravros meta --field stack.framework 2>/dev/null)
TEST_RUNNER=$(~/.claude/bin/bravros meta --field stack.test_runner 2>/dev/null)
# Enriched: exact versions for Context7 docs
FRAMEWORK_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.$FRAMEWORK 2>/dev/null)
TEST_RUNNER_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.$TEST_RUNNER 2>/dev/null)
```

Store the detected runner and use it for all test commands below. When resolving docs via `mcp__context7__resolve-library-id`, include the version in the query (e.g. "pest $TEST_RUNNER_VERSION" instead of just "pest") for version-accurate documentation.

## Core Philosophy

**NEVER mock what you can test.** Use real implementations, real database (SQLite in-memory), real factories. Mocks hide bugs — real integrations catch them.

Only mock when you literally cannot use the real thing:
- External HTTP APIs (use `Http::fake()`)
- Third-party services with no sandbox (payment gateways in production mode)
- Time-dependent behavior (`$this->travel()`)

Everything else — models, services, repositories, jobs, events, notifications, Livewire components — test with real implementations.

## Coverage Goal

- **New projects:** 100% test coverage is the target. Every public method, every branch, every edge case.
- **Older projects:** Improve coverage incrementally. New code must have tests. Existing untested code gets covered as we touch it.
- **Every PR should increase or maintain coverage** — never decrease it.

## Laravel / Pest

### Environment
- **Database:** SQLite in-memory (`:memory:`)
- **Framework:** Pest PHP v4
- **Create:** `php artisan make:test <Path> --pest --no-interaction`
  > **macOS with Herd:** Prefix with `herd` (e.g., `herd php artisan make:test <Path> --pest --no-interaction`)

### Syntax Rules
- ALWAYS `it()` syntax (never `test()`)
- ALWAYS English
- NEVER `describe()` blocks — flat `it()` only

### Structure

```php
<?php

use App\Models\User;

beforeEach(function () {
    // seed reference data if needed
});

it('does something specific', function () {
    $user = User::factory()->create();
    // test logic
});
```

### Factory Best Practices

```php
// BAD: Random values cause flaky tests
$pedido = Pedido::factory()->create();

// GOOD: Explicit values for business-logic fields
$pedido = Pedido::factory()->create([
    'status_code_id' => StatusCode::STATUS_FATURADO,
    'plataforma_id' => Integration::PLATAFORMA_BRAIP,
]);
```

### Livewire Tests (Laravel)
```php
Livewire::actingAs($user)->test(MyComponent::class)
    ->assertOk()
    ->set('name', 'Test')
    ->call('save')
    ->assertHasNoErrors();
```

## Jest / Vitest (React, Next.js, Expo)

### Environment
- **Test Framework:** Jest or Vitest
- **Create:** Run `npx jest` or `npx vitest` to auto-discover tests (no scaffolding tool needed)

### Syntax Rules
- ALWAYS English test descriptions
- Use `describe()` blocks for related tests
- Use `it()` or `test()` interchangeably
- Prefer descriptive test names over comments

### Structure (Jest)
```javascript
import { render, screen } from '@testing-library/react';
import Button from './Button';

describe('Button component', () => {
  it('renders with label', () => {
    render(<Button label="Click me" />);
    expect(screen.getByText('Click me')).toBeInTheDocument();
  });
});
```

### Structure (Vitest)
```javascript
import { describe, it, expect } from 'vitest';
import { add } from './math';

describe('Math utilities', () => {
  it('adds two numbers', () => {
    expect(add(2, 3)).toBe(5);
  });
});
```

### Best Practices
- Use real components/functions; mock only external APIs
- Use `userEvent` or `fireEvent` for realistic interactions
- Test user behavior, not implementation details
- Use data attributes (`data-testid`) for element selection when needed

## pytest (Python)

### Environment
- **Test Framework:** pytest
- **Create:** Create test files following `test_*.py` or `*_test.py` pattern

### Syntax Rules
- Use `def test_<name>()` for test functions
- Use `class Test<Name>:` for grouped tests
- ALWAYS English descriptions

### Structure
```python
import pytest
from app.models import User

def test_create_user():
    user = User(name="Alice", email="alice@example.com")
    user.save()
    assert user.id is not None
    assert user.name == "Alice"

@pytest.fixture
def sample_user():
    return User(name="Bob", email="bob@example.com")

def test_user_email(sample_user):
    assert sample_user.email == "bob@example.com"
```

### Best Practices
- Use fixtures instead of setup/teardown methods
- Use parameterized tests with `@pytest.mark.parametrize`
- Explicit assertion messages: `assert value == expected, "message"`
- Use real database fixtures; mock only external services

## Go test

### Environment
- **Test Framework:** go test (built-in)
- **Create:** Create test files with `*_test.go` suffix in same directory as source

### Syntax Rules
- Use `func TestFunctionName(t *testing.T)` pattern
- Table-driven tests are idiomatic

### Structure
```go
package user

import "testing"

func TestCreateUser(t *testing.T) {
    user := NewUser("Alice")
    if user.Name != "Alice" {
        t.Errorf("Expected Alice, got %s", user.Name)
    }
}

// Table-driven test (idiomatic Go)
func TestUserValidation(t *testing.T) {
    tests := []struct {
        name      string
        email     string
        wantError bool
    }{
        {"valid email", "alice@example.com", false},
        {"invalid email", "invalid", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := NewUser(tt.email)
            if (err != nil) != tt.wantError {
                t.Errorf("unexpected error: %v", err)
            }
        })
    }
}
```

### Best Practices
- Use table-driven tests for multiple cases
- Prefer explicit assertions over assertion libraries
- Use `t.Run()` for subtests
- Keep test files in same package as source code

## What to Test (Checklist)

For every piece of code, consider:

1. **Happy path** — does it work as intended?
2. **Validation** — does it reject bad input?
3. **Authorization** — does it block unauthorized users?
4. **Edge cases** — null values, empty collections, boundary conditions
5. **Error handling** — what happens when things fail?
6. **Side effects** — does it dispatch jobs, send notifications, fire events?
7. **State changes** — does it create/update/delete the right records?

## Don't Mock These — Test Them For Real

### Laravel / Pest
| Instead of mocking... | Do this |
|----------------------|---------|
| Eloquent models | Use factories with `create()` |
| FormRequest validation | Submit real request, assert validation errors |
| Livewire components | Use `Livewire::test()` with real props |
| Jobs & queues | Use `Queue::fake()` only to assert dispatch, test job logic directly |
| Events | Use `Event::fake()` only to assert dispatch, test listener logic directly |
| Notifications | Use `Notification::fake()` to assert sent, test content directly |
| Mail | Use `Mail::fake()` to assert sent, test mailable content directly |
| Services/repositories | Use real instances with real database |
| File uploads | Use `UploadedFile::fake()` (this IS the real Laravel way) |

### Jest / Vitest (React)
| Instead of mocking... | Do this |
|----------------------|---------|
| React components | Render with `@testing-library/react` |
| API calls | Use `MSW` (Mock Service Worker) for realistic request/response |
| User interactions | Use `userEvent` library for realistic behavior |
| Context/Redux state | Render with real providers |
| External libraries | Mock only 3rd-party APIs; test your own code |

### pytest (Python)
| Instead of mocking... | Do this |
|----------------------|---------|
| Database models | Use real in-memory database or fixtures |
| Form validation | Test with real form classes |
| Services | Use real service instances with test fixtures |
| API calls | Use `pytest-httpserver` or `responses` library |
| File I/O | Use `tmp_path` fixture for real file operations |

### Go test
| Instead of mocking... | Do this |
|----------------------|---------|
| Database | Use real in-memory SQLite or test database |
| Functions | Call real functions; avoid interfaces unless truly necessary |
| HTTP calls | Use `httptest` package for real server testing |
| Goroutines | Test real concurrent behavior with channels |
| Error handling | Test real error types and error wrapping |

## After Creating

1. **Run targeted test using detected runner:**
   - Pest: `vendor/bin/pest tests/Feature/Path/ToNewTest.php`
   - Jest/Vitest: `npx jest --testPathPattern="Path/ToNewTest"`
   - pytest: `pytest tests/test_module.py`
   - Go test: `go test ./package -run TestName`

2. **Full suite:** Ask user to run the project's full test command in a separate terminal
   - Laravel: `ptp` (alias for full parallel run)
   - JS: `npm test` or `npx jest`
   - Python: `pytest tests/`
   - Go: `go test ./...`

3. **Coverage check:** Ask user to run the project's coverage command in a separate terminal
   - Laravel: `tcq` (alias for full coverage)
   - JS: `npx jest --coverage`
   - Python: `pytest --cov`
   - Go: `go test -cover ./...`

## Rules

- NEVER mock what you can test with real implementations
- NEVER run the full test suite — ask user to run it in separate terminal
- NEVER run full coverage — ask user to run it in separate terminal
- ALWAYS follow the project's test framework conventions
- ALWAYS set explicit values for business-logic factory fields
- ALWAYS aim for 100% coverage on new projects
- ALWAYS test validation rules, authorization, and edge cases
- All user interactions MUST use `AskUserQuestion` tool, never plain text questions

Use $ARGUMENTS as the code path, class name, or feature to test.
