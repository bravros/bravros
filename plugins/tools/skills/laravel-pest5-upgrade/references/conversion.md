# PHPUnit class-style → Pest conversion

Read this while doing step 2. The finish line is **zero class declarations in collected test
files** — Pest's TIA guard panics on any test class lacking its `__initializeTestCase` marker, and
it aborts the whole run rather than skipping the file.

## Find them

```bash
grep -rln --include='*Test.php' -E '^\s*(final\s+)?(abstract\s+)?class\s+\w+Test\b' tests/
```

Note what this deliberately does **not** match: abstract base classes and helper classes that
aren't named `*Test`. Those are fine — the guard only fires on classes PHPUnit actually prepares
as tests. Verify by re-running the grep at the end; if it's empty, you're done.

## The mapping

| PHPUnit | Pest | Notes |
|---|---|---|
| `class FooTest extends TestCase` | *(delete)* | The file becomes a flat list of `it()` / `test()` closures |
| `setUp(): void` | `beforeEach(function () {...})` | `$this->foo = ...` still works — Pest marks its generated case `#[AllowDynamicProperties]` |
| `tearDown(): void` | `afterEach(function () {...})` | |
| `public function test_foo()` | `it('foo', function () {...})` | Rename to read as a sentence; the method name was a slug, the `it()` string is prose |
| `protected function helper()` | file-scoped `function prefixedHelper()` | **See the collision trap below** |
| `use RefreshDatabase;` | *(delete)* | Only if `tests/Pest.php` applies it globally — check first |
| `$this->assertSame(...)` | works unchanged | Closures are bound to the TestCase |
| `$this->expectExceptionObject(...)` | works unchanged | Same reason |
| `@dataProvider` / `#[DataProvider]` | `->with([...])` | See "Collapsing into datasets" |

## The helper-collision trap

`protected function mockSomeApi()` was scoped to its class. The moment it becomes a file-scoped
function it is **global across the entire suite**, and two files defining the same name is a fatal
redeclare — which surfaces as an unrelated file failing.

On the reference migration a bare `mockCorreiosApi()` already existed in another test file, so the
converted helper became `agregadoFreightMockCorreiosApi()`. Prefix with the subject under test, not
with `test` or `helper`.

Check before you name it:

```bash
grep -rn "^function <name>" tests/
```

**Where the function lives matters too.** If a helper is needed by more than one test file, a
file-scoped function is the wrong home: parallel runners shard test files across processes, so a
function defined in file A is undefined in the process running file B. Cross-file helpers belong in
the always-loaded `tests/Pest.php`. Class-shaped helpers belong in their own PSR-4 file under
`tests/Fakes/` or `tests/Helpers/` — a named class inside a `*Test.php` breaks PSR-4 and gets
silently skipped by the optimized autoloader.

## Collapsing into datasets

Worth doing where several methods differ only by input. Not worth forcing.

```php
// before: two methods, one a strict subset of the other
public function test_notification_uses_emails_queue()        { /* checks AlteracaoDeSenha */ }
public function test_priority_notifications_use_queue()      { /* loops 3 notifications */ }

// after: one test, three named cases
it('queues time-sensitive notifications on the priority queue', function (object $n) {
    expect($n->queue)->toBe('emails-priority');
})->with([
    'AlteracaoDeSenha' => fn () => new AlteracaoDeSenha('test'),
    'NovoUsuario'      => fn () => new NovoUsuario('test'),
    'ResetPassword'    => fn () => new ResetPassword('test-token'),
]);
```

That merge is legitimate — the first method tested one of the three cases the second already
covered. **Use string keys**, so a failure reports which case broke instead of `dataset #2`.

The line to hold: a merge is fine when the removed method's assertions are genuinely a subset.
Dropping a case because it "looked similar" is a silent coverage loss that no checkpoint catches.

## Verifying the conversion

Per file, compare against the pre-conversion run:

```bash
vendor/bin/pest tests/Feature/Path/ToTest.php --compact
```

Assertion count is the useful signal, not test count — merging into a dataset legitimately changes
the test count while the assertion count should hold or grow. On the reference migration the two
converted files landed at 15 passed / 26 assertions and 8 passed / 20 assertions, each matching
what the class-style versions asserted.

## What does not need converting

- Abstract base classes under `tests/` that aren't collected as tests.
- `tests/Fakes/`, `tests/Helpers/`, factories, seeders — these are ordinary PSR-4 classes.
- `tests/Pest.php`, `tests/TestCase.php` — infrastructure, not collected tests.
