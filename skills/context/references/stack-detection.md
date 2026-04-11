# Stack Detection Reference

Reference file bundled with the `context` skill. Used in Step 1 (Detect Stack).

## 1a. Language Detection

Check for these files in the project root to determine the primary language:

| File | Language |
|------|----------|
| `composer.json` | PHP |
| `package.json` | Node / JavaScript / TypeScript |
| `go.mod` | Go |
| `Gemfile` | Ruby |
| `requirements.txt` / `pyproject.toml` / `setup.py` | Python |
| `Cargo.toml` | Rust |
| `pom.xml` / `build.gradle` | Java / Kotlin |
| `*.csproj` / `*.sln` | C# / .NET |
| `mix.exs` | Elixir |
| `pubspec.yaml` | Dart / Flutter |

If multiple are found, the project is polyglot — note all languages detected.

## 1b. Framework + Version Detection

Read the appropriate lock file for exact versions:

**PHP** (`composer.lock`):
- `laravel/framework` → Laravel (check version)
- `livewire/livewire` → Livewire
- `filament/filament` → Filament
- `symfony/symfony` → Symfony
- `laravel/jetstream`, `laravel/breeze` → auth scaffolding

**Node/JS** (`package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`):
- `next` → Next.js
- `react` → React
- `vue` → Vue
- `@angular/core` → Angular
- `svelte` → Svelte / SvelteKit
- `express` → Express
- `nuxt` → Nuxt
- `astro` → Astro
- `expo` → Expo / React Native

**Go** (`go.sum`):
- `github.com/gin-gonic/gin` → Gin
- `github.com/labstack/echo` → Echo
- `github.com/gofiber/fiber` → Fiber

**Ruby** (`Gemfile.lock`):
- `rails` → Ruby on Rails

**Python** (`requirements.txt` / `pyproject.toml`):
- `django` → Django
- `flask` → Flask
- `fastapi` → FastAPI

**Rust** (`Cargo.toml`):
- `actix-web` → Actix
- `axum` → Axum
- `rocket` → Rocket

## 1c. Test Runner Detection

| Marker | Test Runner |
|--------|-------------|
| `pest` in composer.json (require-dev) | Pest PHP |
| `phpunit/phpunit` in composer.json | PHPUnit |
| `jest` in package.json | Jest |
| `vitest` in package.json | Vitest |
| `cypress` in package.json | Cypress |
| `playwright` in package.json | Playwright |
| `pytest` in requirements.txt / pyproject.toml | pytest |
| `_test.go` files exist | Go test |
| `rspec` in Gemfile | RSpec |
| `minitest` in Gemfile | Minitest |
| `#[cfg(test)]` in Rust files | Cargo test |

## 1d. Asset Pipeline Detection

| Marker | Tool |
|--------|------|
| `vite.config.*` | Vite |
| `webpack.config.*` | Webpack |
| `esbuild` in package.json | esbuild |
| `tailwind.config.*` | Tailwind CSS |
| `postcss.config.*` | PostCSS |

## 1e. Database Detection

Check `.env`, `.env.example`, or config files for:
- `DB_CONNECTION=mysql` or `DATABASE_URL=mysql` → MySQL
- `DB_CONNECTION=pgsql` or `DATABASE_URL=postgres` → PostgreSQL
- `DB_CONNECTION=sqlite` → SQLite
- `MONGODB_URI` or `mongoose` in package.json → MongoDB
- `REDIS_HOST` or `redis` dependency → Redis

## 1f. Output Summary

After detection, echo a summary:

```bash
echo "[context:1] Stack detected:"
echo "  Language:    PHP 8.3"
echo "  Framework:   Laravel 11.x + Livewire 3.x"
echo "  Test runner: Pest PHP"
echo "  Assets:      Vite + Tailwind CSS"
echo "  Database:    MySQL + Redis"
```
