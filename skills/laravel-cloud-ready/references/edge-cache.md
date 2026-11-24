# Edge caching — middleware, rules, tests

Read before writing the edge-cache middleware for `/laravel-cloud-ready` § 3.

## Why each rule exists

- Laravel Cloud caches HTML only when the app says so and **refuses any response carrying
  `Set-Cookie`**. Remove the header (this also empties Symfony's cookie bag — what actually
  unblocks the edge), do not merely set `Cache-Control`.
- Livewire's `DisableBackButtonCacheMiddleware` stamps `no-store`, `Pragma` and a 1990 `Expires`
  on every full-page component. Laravel Cloud honours all three — clear all three.
- `prepend()` to the **global** stack so the middleware unwinds last: after the session cookie is
  queued and encrypted, and after Livewire's globally pushed middleware.
- **`s-maxage` only.** Laravel Cloud purges the edge on every deployment, so an edge copy never
  outlives the deploy that replaced it. A private browser copy gets no purge — hence
  `max-age=0, must-revalidate`. Revalidation is answered by the edge, so hibernation still works.

## Header block

```php
$response->headers->remove('Set-Cookie');
$response->headers->remove('Pragma');
$response->headers->remove('Expires');
$response->headers->set('Cache-Control', 'public, max-age=0, s-maxage=3600, must-revalidate');
```

## Never cache

| Excluded | Why |
|---|---|
| Livewire form routes | one shared CSRF token vs each visitor's own session ⇒ 419 on every post |
| Authenticated requests | leaks one user's page to everyone |
| Non-200, non-cacheable methods | caches errors |
| UTM-tagged requests | a cached response sets no cookie, so no session exists to record attribution |

**Strip the CSRF meta tag** from cacheable HTML. `<meta name="csrf-token">` bakes in the token of
whichever request populated the edge. Inert while no JS reads it — then silently 419s for a whole
TTL window the moment some does. An absent tag fails loudly; a wrong one fails for an hour. After
`setContent()`, remove `Content-Length` — a stale length truncates the body.

## Middleware sketch

```php
<?php

declare(strict_types=1);

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\Response;

class EdgeCache
{
    /** Route names that must never be edge-cached (form posts, Livewire forms). */
    private const EXCLUDED_ROUTES = ['contact', 'newsletter.subscribe'];

    public function handle(Request $request, Closure $next): Response
    {
        $response = $next($request);

        if (! $this->cacheable($request, $response)) {
            return $response;
        }

        $content = preg_replace('/<meta name="csrf-token"[^>]*>\s*/i', '', (string) $response->getContent());
        $response->setContent($content);
        $response->headers->remove('Content-Length');

        $response->headers->remove('Set-Cookie');
        $response->headers->remove('Pragma');
        $response->headers->remove('Expires');
        $response->headers->set('Cache-Control', 'public, max-age=0, s-maxage=3600, must-revalidate');

        return $response;
    }

    private function cacheable(Request $request, Response $response): bool
    {
        return $request->isMethodCacheable()
            && $response->getStatusCode() === 200
            && ! $request->user()
            && ! $request->query->has('utm_source')
            && ! in_array($request->route()?->getName(), self::EXCLUDED_ROUTES, true);
    }
}
```

Register with `$middleware->prepend(EdgeCache::class)` in `bootstrap/app.php`.

## Feature tests

```php
it('makes a public page edge-cacheable', function () {
    $response = $this->get('/');

    expect($response->headers->get('Cache-Control'))->toContain('s-maxage=3600');
    expect($response->headers->has('Set-Cookie'))->toBeFalse();
    expect($response->getContent())->not->toContain('name="csrf-token"');
});

it('never caches authenticated pages', function () {
    $this->actingAs(User::factory()->create())
        ->get('/')
        ->assertHeaderMissing('Cache-Control', 'public');
});

it('never caches utm-tagged requests', function () {
    $this->get('/?utm_source=ads')
        ->assertHeader('Set-Cookie');
});
```

Adapt route names and the exclusion list to the app; the tests above are the minimum set.
