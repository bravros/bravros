---
name: laravel-cloud-ready
description: Prepare a Laravel app for Laravel Cloud — SQS FIFO message groups, edge-cacheable public pages, Horizon removal. Use on `/laravel-cloud-ready`, or when a deploy hits MissingParameter MessageGroupId.
---

# laravel-cloud-ready

INTENT: close the three gaps that break a Laravel app the first time it runs on Laravel Cloud —
FIFO queues, edge caching, and Horizon — before the deploy finds them.

Verified against **Laravel 13.25.0**. The mailable/notification asymmetry in § FIFO is
framework-internal; re-read the vendor source before trusting it on a newer major.

## 0. Preflight — before touching anything

```bash
composer show aws/aws-sdk-php >/dev/null 2>&1; echo "aws-sdk rc=$?"
```

**`aws/aws-sdk-php` must be a composer dependency.** Laravel Cloud's managed queue is SQS
underneath, and a deploy without this package fails with "Your application has a managed queue
but is missing the [aws/aws-sdk-php] package." rc≠0 ⇒ `composer require aws/aws-sdk-php` now,
before the first deploy that enables the managed queue, not after the failure.

## 1. SQS FIFO — three paths, three different hooks

Laravel Cloud's managed queue is a `.fifo` queue. Every queued object must carry a message group
or the push dies with:

```
MissingParameter (Sender): The request must contain the parameter MessageGroupId
```

`SqsQueue::getQueueableOptions()` inspects **the object actually pushed to the queue** — reading
`$job->messageGroup` (property) first, then `$job->messageGroup()` (method). Which object that is
differs per path, and that is the whole trap:

| Path | Object SQS inspects | Hook | `messageGroup()` on the class? |
|---|---|---|---|
| Job | the job itself | `messageGroup()` | ✅ read directly |
| **Mailable** | `SendQueuedMailable` wrapper | override `newQueuedJob()` → `->onGroup()` | ❌ **silently ignored** |
| Notification | `SendQueuedNotifications` wrapper | `messageGroup()` | ✅ `NotificationSender` forwards it |

**Jobs** — plain method:

```php
public function messageGroup(): string
{
    return 'ebook-'.$this->ebookId;
}
```

**Mailables** — a `messageGroup()` on the mailable is never read. `SendQueuedMailable`'s
constructor copies `connection`, `queue`, `tries`, `timeout`, and `maxExceptions` from the
mailable — but not the group. Overriding the wrapper's construction is the only attachment point:

```php
trait GroupsQueuedMail
{
    abstract public function messageGroup(): string;

    protected function newQueuedJob()
    {
        return parent::newQueuedJob()->onGroup($this->messageGroup());
    }
}
```

**Notifications** — no trait needed, and this asymmetry is the part people get wrong in both
directions. `NotificationSender::queueNotification()` already reads `messageGroup()` off the
notification and forwards it via `->onGroup()` to the wrapper. Just declare the method.

**Queued listeners** — declare `messageGroup()` too, and confirm in the vendor source that the
event dispatcher forwards it to `CallQueuedListener` on the installed Laravel version.

**Choosing the group key.** Group per entity (`post-{id}`, `lead-{id}`, `media-{id}`), never a
constant — a single group serializes everything in it. Deliberately *share* a key only when
ordering matters: a lead's download link, follow-up mail, and scheduled sends all belong in
`lead-{id}` so sequence 1 precedes sequence 2.

## 2. The guard test — recursive, every queued folder, never vacuous

A hand-written dataset only proves the classes someone remembered to list. Walk the source tree
instead: **recursively** (subfolders), over **every** folder that holds queued classes — Jobs,
Notifications, Listeners, Mail. Mailables are held to a different rule: they must use the
`GroupsQueuedMail` trait, because a `messageGroup()` on a mailable is never read.

```php
use App\Concerns\GroupsQueuedMail;
use Illuminate\Contracts\Queue\ShouldQueue;
use Symfony\Component\Finder\Finder;

/** @return list<class-string> every class under app/<dir>, recursively */
function classesUnder(string $dir, string $ns): array
{
    if (! is_dir(app_path($dir))) {
        return [];
    }

    $classes = [];
    foreach ((new Finder)->files()->in(app_path($dir))->name('*.php') as $file) {
        $relative = str_replace(['/', '.php'], ['\\', ''], $file->getRelativePathname());
        $classes[] = $ns.'\\'.$relative;
    }

    return $classes;
}

dataset('queued-folders', [
    'Jobs' => ['Jobs', 'App\\Jobs'],
    'Notifications' => ['Notifications', 'App\\Notifications'],
    'Listeners' => ['Listeners', 'App\\Listeners'],
    'Mail' => ['Mail', 'App\\Mail'],
]);

it('scans a non-empty folder', function (string $dir, string $ns) {
    // Non-vacuous: a glob/namespace typo or a deleted folder fails here instead of passing forever.
    expect(classesUnder($dir, $ns))->not->toBeEmpty("app/{$dir} has no classes — wrong path or namespace?");
})->with('queued-folders');

it('leaves no queued class without a message group', function (string $dir, string $ns) {
    $queued = collect(classesUnder($dir, $ns))
        ->filter(fn (string $class) => class_exists($class) && is_subclass_of($class, ShouldQueue::class));

    $missing = $dir === 'Mail'
        ? $queued->reject(fn (string $class) => in_array(GroupsQueuedMail::class, class_uses_recursive($class), true))
        : $queued->reject(fn (string $class) => method_exists($class, 'messageGroup'));

    expect($missing->values()->all())->toBeEmpty("Missing message group: {$missing->implode(', ')}");
})->with('queued-folders');
```

Drop a dataset row only when the app genuinely has no such folder — and say so in the test file.

Confirm once that the enumeration sees the real queued classes:

```bash
php artisan tinker --execute 'echo collect(File::allFiles(app_path("Jobs")))->count()." job files";'
```

Assert the wrapper too, not just the method — the method is not what SQS reads:

```php
Queue::fake();
$lead->notify(new EbookDownloadLink($ebook, $activity));
Queue::assertPushed(SendQueuedNotifications::class,
    fn ($job) => $job->messageGroup === 'lead-'.$lead->id);
```

## 3. Edge caching — the point is hibernation

Requests served at the edge never reach compute, so the environment sleeps instead of being woken
by crawlers. Laravel Cloud caches HTML only when the app says so, and **refuses any response
carrying `Set-Cookie`** — which Laravel queues on every web response. Two non-obvious blockers:

- **`Set-Cookie` must be removed**, not just `Cache-Control` set. Removing the header also empties
  Symfony's cookie bag, which is what actually unblocks the edge.
- **Livewire's `DisableBackButtonCacheMiddleware`** stamps `no-store` plus `Pragma` and a 1990
  `Expires` on every full-page component. Laravel Cloud honours all three — clear all three.

`prepend()` to the **global** stack so the middleware unwinds last. **`s-maxage` only** —
`Cache-Control: public, max-age=0, s-maxage=3600, must-revalidate`; the edge is purged on every
deploy, a private browser copy is not.

**Never cache:** Livewire form routes (shared CSRF token ⇒ 419 on every post) · authenticated
requests (leaks one user's page) · non-200 / non-cacheable methods · UTM-tagged requests (no
session exists to record attribution). **Strip the CSRF meta tag** from cacheable HTML and remove
`Content-Length` after `setContent()`.

The middleware, its feature tests, and the rationale for each rule:
**`references/edge-cache.md`** — read it before writing the middleware.

## 4. Horizon is incompatible

Laravel Cloud manages queue workers itself. **Grep for leftovers first** — the starter kit ships
Horizon, so references hide in more places than the composer dep:

```bash
grep -rniE 'horizon' composer.json config app routes bootstrap resources database tests .env.example
```

Remove `laravel/horizon`, `config/horizon.php`, the `HorizonServiceProvider` (and its entry in
`bootstrap/providers.php`), the Horizon panel/nav/gate registration, any `horizon:snapshot`
schedule entry, `HORIZON_*` env keys, and Horizon-specific tests. Re-run the grep until it is
empty. Removing the composer dep means production needs a `composer install` on deploy.

## Verify

```bash
php artisan route:list --except-vendor >/dev/null; echo "boot rc=$?"   # boots without Horizon
composer show aws/aws-sdk-php >/dev/null 2>&1; echo "aws-sdk rc=$?"    # step 0 requirement still holds
```

Run the guard test by path (`php artisan test --compact tests/Feature/QueueGroupsTest.php`) —
never the full suite yourself in an interactive session; hand the operator the full-suite command
for a separate tab (background it only when AFK/autonomous).

**Scale-to-zero / hibernation wake interval is a dashboard setting, not code.** The hourly (or
other) wake cadence that keeps a hibernating environment checking in is configured in the Laravel
Cloud dashboard for that environment — there is no config file, env var, or code path to search
for it. Point the operator at the dashboard instead of hunting the repo.

Post-deploy, confirm on a cacheable route: `cache-control: public, max-age=0, s-maxage=3600,
must-revalidate`, no `set-cookie`, no `csrf-token` meta — and that a form route still posts
without a 419, a UTM landing still records attribution, and one mail plus one notification clear
the `.fifo` queue.
